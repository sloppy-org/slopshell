using System.Diagnostics;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

const int DatasetSize = 512;
const int ThroughputOps = 300_000;
const int LatencyOps = 50_000;

var payloads = MakePayloads();
ulong checksum = 0;

var throughputSw = Stopwatch.StartNew();
for (var i = 0; i < ThroughputOps; i++)
{
    var result = Handle(payloads[i % DatasetSize]);
    checksum += (ulong)result.Length + (ulong)result.First;
}
throughputSw.Stop();
var throughputOpsPerSec = ThroughputOps / throughputSw.Elapsed.TotalSeconds;

var samples = new List<double>(LatencyOps);
for (var i = 0; i < LatencyOps; i++)
{
    var raw = payloads[(i * 7) % DatasetSize];
    var start = Stopwatch.GetTimestamp();
    var result = Handle(raw);
    var elapsed = Stopwatch.GetElapsedTime(start);
    var us = elapsed.TotalMilliseconds * 1000.0;
    samples.Add(us);
    checksum += (ulong)result.Length + (ulong)result.First;
}
samples.Sort();

var output = new
{
    runtime = "csharp",
    dataset_size = DatasetSize,
    throughput_ops = ThroughputOps,
    latency_samples = LatencyOps,
    throughput_ops_per_sec = throughputOpsPerSec,
    latency_us = new
    {
        p50 = Percentile(samples, 0.50),
        p95 = Percentile(samples, 0.95),
        p99 = Percentile(samples, 0.99)
    },
    checksum
};

Console.WriteLine(JsonSerializer.Serialize(output));

return;

static string[] MakePayloads()
{
    var actions = new[]
    {
        "review this patch",
        "switch project alpha",
        "cancel active turn",
        "open pr review",
        "summarize the latest logs"
    };
    var payloads = new string[DatasetSize];
    for (var i = 0; i < DatasetSize; i++)
    {
        var action = actions[i % actions.Length];
        var repeat = 4 + (i % 9);
        var parts = new List<string>(repeat);
        for (var j = 0; j < repeat; j++)
        {
            parts.Add($"token{i % 17}_{j}");
        }
        var req = new RequestPayload
        {
            TurnId = $"turn-{i:000000}",
            ProjectKey = $"/workspace/proj-{i % 11:00}",
            Message = $"Please {action} while handling backend request {i % 97}. {string.Join(' ', parts)}",
            ChatMode = "chat",
            RecentFiles =
            [
                "internal/web/chat.go",
                "internal/web/server.go",
                "internal/web/static/app.js"
            ],
            Flags = new Dictionary<string, bool>
            {
                ["silent"] = i % 3 == 0,
                ["conversation"] = i % 2 == 0
            },
            Meta = new Dictionary<string, string>
            {
                ["branch"] = "fix/tap-stop-working",
                ["model"] = "spark"
            },
            Timestamp = 1_700_000_000L + i
        };
        payloads[i] = JsonSerializer.Serialize(req);
    }

    return payloads;
}

static string DetectIntent(string normalized)
{
    if (normalized.Contains("open pr", StringComparison.Ordinal) || normalized.Contains("review", StringComparison.Ordinal))
        return "open_pr_review";
    if (normalized.Contains("switch project", StringComparison.Ordinal))
        return "switch_project";
    if (normalized.Contains("cancel", StringComparison.Ordinal))
        return "cancel_turn";
    return "chat";
}

static HandleResult Handle(string raw)
{
    var req = JsonSerializer.Deserialize<RequestPayload>(raw) ?? throw new InvalidOperationException("request parse failed");
    var normalized = req.Message.Trim().ToLowerInvariant();
    var tokenCount = string.IsNullOrWhiteSpace(normalized) ? 0 : normalized.Split((char[])null!, StringSplitOptions.RemoveEmptyEntries).Length;
    var digest = SHA256.HashData(Encoding.UTF8.GetBytes($"{req.ProjectKey}|{req.TurnId}|{normalized}"));
    var hashPrefix = Convert.ToHexString(digest.AsSpan(0, 8)).ToLowerInvariant();

    var resp = new ResponsePayload
    {
        Ok = true,
        TurnId = req.TurnId,
        Intent = DetectIntent(normalized),
        TokenCount = tokenCount,
        RenderOnCanvas = tokenCount > 30 || normalized.Contains("diff", StringComparison.Ordinal),
        HashPrefix = hashPrefix
    };
    var outJson = JsonSerializer.Serialize(resp);
    var first = outJson.Length > 0 ? outJson[0] : (char)0;
    return new HandleResult(outJson.Length, first);
}

static double Percentile(List<double> sortedSamples, double p)
{
    if (sortedSamples.Count == 0) return 0.0;
    if (p <= 0) return sortedSamples[0];
    if (p >= 1) return sortedSamples[^1];
    var idx = (int)Math.Floor(p * (sortedSamples.Count - 1));
    return sortedSamples[idx];
}

file sealed class RequestPayload
{
    [JsonPropertyName("turn_id")]
    public string TurnId { get; set; } = "";

    [JsonPropertyName("project_key")]
    public string ProjectKey { get; set; } = "";

    [JsonPropertyName("message")]
    public string Message { get; set; } = "";

    [JsonPropertyName("chat_mode")]
    public string ChatMode { get; set; } = "";

    [JsonPropertyName("recent_files")]
    public List<string> RecentFiles { get; set; } = new();

    [JsonPropertyName("flags")]
    public Dictionary<string, bool> Flags { get; set; } = new();

    [JsonPropertyName("meta")]
    public Dictionary<string, string> Meta { get; set; } = new();

    [JsonPropertyName("timestamp")]
    public long Timestamp { get; set; }
}

file sealed class ResponsePayload
{
    [JsonPropertyName("ok")]
    public bool Ok { get; set; }

    [JsonPropertyName("turn_id")]
    public string TurnId { get; set; } = "";

    [JsonPropertyName("intent")]
    public string Intent { get; set; } = "";

    [JsonPropertyName("token_count")]
    public int TokenCount { get; set; }

    [JsonPropertyName("render_on_canvas")]
    public bool RenderOnCanvas { get; set; }

    [JsonPropertyName("hash_prefix")]
    public string HashPrefix { get; set; } = "";
}

file readonly record struct HandleResult(int Length, char First);
