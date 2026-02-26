import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Locale;

public final class Main {
    private static final int DATASET_SIZE = 512;
    private static final int THROUGHPUT_OPS = 300_000;
    private static final int LATENCY_OPS = 50_000;

    private record ParsedRequest(String turnId, String projectKey, String message) {}

    private static List<String> makePayloads() {
        List<String> actions = Arrays.asList(
            "review this patch",
            "switch project alpha",
            "cancel active turn",
            "open pr review",
            "summarize the latest logs"
        );
        List<String> payloads = new ArrayList<>(DATASET_SIZE);
        for (int i = 0; i < DATASET_SIZE; i += 1) {
            String action = actions.get(i % actions.size());
            int repeat = 4 + (i % 9);
            StringBuilder tokens = new StringBuilder();
            for (int j = 0; j < repeat; j += 1) {
                if (j > 0) tokens.append(' ');
                tokens.append("token").append(i % 17).append('_').append(j);
            }
            String turnId = String.format(Locale.ROOT, "turn-%06d", i);
            String projectKey = String.format(Locale.ROOT, "/workspace/proj-%02d", i % 11);
            String message = String.format(
                Locale.ROOT,
                "Please %s while handling backend request %d. %s",
                action,
                i % 97,
                tokens
            );
            String payload = "{"
                + "\"turn_id\":\"" + escape(turnId) + "\","
                + "\"project_key\":\"" + escape(projectKey) + "\","
                + "\"message\":\"" + escape(message) + "\","
                + "\"chat_mode\":\"chat\","
                + "\"recent_files\":[\"internal/web/chat.go\",\"internal/web/server.go\",\"internal/web/static/app.js\"],"
                + "\"flags\":{\"silent\":" + ((i % 3) == 0 ? "true" : "false") + ",\"conversation\":" + ((i % 2) == 0 ? "true" : "false") + "},"
                + "\"meta\":{\"branch\":\"fix/tap-stop-working\",\"model\":\"spark\"},"
                + "\"timestamp\":" + (1_700_000_000L + i)
                + "}";
            payloads.add(payload);
        }
        return payloads;
    }

    private static String escape(String text) {
        return text
            .replace("\\", "\\\\")
            .replace("\"", "\\\"");
    }

    private static String detectIntent(String normalized) {
        if (normalized.contains("open pr") || normalized.contains("review")) {
            return "open_pr_review";
        }
        if (normalized.contains("switch project")) {
            return "switch_project";
        }
        if (normalized.contains("cancel")) {
            return "cancel_turn";
        }
        return "chat";
    }

    // Lightweight JSON field extraction for fixed benchmark schema.
    private static String extractJsonString(String raw, String key) {
        String marker = "\"" + key + "\":\"";
        int start = raw.indexOf(marker);
        if (start < 0) return "";
        int cursor = start + marker.length();
        StringBuilder out = new StringBuilder();
        boolean escaping = false;
        while (cursor < raw.length()) {
            char ch = raw.charAt(cursor);
            cursor += 1;
            if (escaping) {
                out.append(ch);
                escaping = false;
                continue;
            }
            if (ch == '\\') {
                escaping = true;
                continue;
            }
            if (ch == '"') {
                break;
            }
            out.append(ch);
        }
        return out.toString();
    }

    private static ParsedRequest decodeRequest(String raw) {
        String turnId = extractJsonString(raw, "turn_id");
        String projectKey = extractJsonString(raw, "project_key");
        String message = extractJsonString(raw, "message");
        return new ParsedRequest(turnId, projectKey, message);
    }

    private static String shaPrefix(String text) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(text.getBytes(StandardCharsets.UTF_8));
            StringBuilder out = new StringBuilder();
            for (int i = 0; i < 8 && i < hash.length; i += 1) {
                out.append(String.format(Locale.ROOT, "%02x", hash[i]));
            }
            return out.toString();
        } catch (NoSuchAlgorithmException err) {
            throw new RuntimeException(err);
        }
    }

    private static long[] handle(String raw) {
        ParsedRequest req = decodeRequest(raw);
        String normalized = req.message().trim().toLowerCase(Locale.ROOT);
        int tokenCount = normalized.isEmpty() ? 0 : normalized.split("\\s+").length;
        String digest = shaPrefix(req.projectKey() + "|" + req.turnId() + "|" + normalized);
        String intent = detectIntent(normalized);
        boolean renderOnCanvas = tokenCount > 30 || normalized.contains("diff");

        String response = "{"
            + "\"ok\":true,"
            + "\"turn_id\":\"" + escape(req.turnId()) + "\","
            + "\"intent\":\"" + intent + "\","
            + "\"token_count\":" + tokenCount + ","
            + "\"render_on_canvas\":" + (renderOnCanvas ? "true" : "false") + ","
            + "\"hash_prefix\":\"" + digest + "\""
            + "}";

        int first = response.isEmpty() ? 0 : response.charAt(0);
        return new long[] {response.length(), first};
    }

    private static double percentile(List<Double> sortedSamples, double p) {
        if (sortedSamples.isEmpty()) return 0.0;
        if (p <= 0.0) return sortedSamples.get(0);
        if (p >= 1.0) return sortedSamples.get(sortedSamples.size() - 1);
        int idx = (int) Math.floor(p * (sortedSamples.size() - 1));
        return sortedSamples.get(idx);
    }

    public static void main(String[] args) {
        List<String> payloads = makePayloads();
        long checksum = 0L;

        long throughputStart = System.nanoTime();
        for (int i = 0; i < THROUGHPUT_OPS; i += 1) {
            long[] result = handle(payloads.get(i % DATASET_SIZE));
            checksum += result[0] + result[1];
        }
        double throughputSeconds = (System.nanoTime() - throughputStart) / 1_000_000_000.0;

        List<Double> latencySamples = new ArrayList<>(LATENCY_OPS);
        for (int i = 0; i < LATENCY_OPS; i += 1) {
            String raw = payloads.get((i * 7) % DATASET_SIZE);
            long t0 = System.nanoTime();
            long[] result = handle(raw);
            double dtUs = (System.nanoTime() - t0) / 1000.0;
            latencySamples.add(dtUs);
            checksum += result[0] + result[1];
        }
        Collections.sort(latencySamples);

        double throughput = THROUGHPUT_OPS / throughputSeconds;
        String out = "{"
            + "\"runtime\":\"java\","
            + "\"dataset_size\":" + DATASET_SIZE + ","
            + "\"throughput_ops\":" + THROUGHPUT_OPS + ","
            + "\"latency_samples\":" + LATENCY_OPS + ","
            + "\"throughput_ops_per_sec\":" + throughput + ","
            + "\"latency_us\":{"
            + "\"p50\":" + percentile(latencySamples, 0.50) + ","
            + "\"p95\":" + percentile(latencySamples, 0.95) + ","
            + "\"p99\":" + percentile(latencySamples, 0.99)
            + "},"
            + "\"checksum\":" + checksum
            + "}";
        System.out.println(out);
    }
}
