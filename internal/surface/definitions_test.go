package surface

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var webRoutePattern = regexp.MustCompile(`r\.(Get|Post|Put|Patch|Delete)\("([^"]+)"`)

func TestWebRouteDefinitionsStayInSyncWithRouter(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	serverPath := filepath.Join(filepath.Dir(file), "..", "web", "server.go")
	content, err := os.ReadFile(serverPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", serverPath, err)
	}

	routerRoutes := map[string]struct{}{}
	for _, match := range webRoutePattern.FindAllStringSubmatch(string(content), -1) {
		route := strings.ToUpper(match[1]) + " " + match[2]
		routerRoutes[route] = struct{}{}
	}

	definedRoutes := map[string]struct{}{}
	for _, section := range WebRouteSections {
		for _, route := range section.Routes {
			definedRoutes[route] = struct{}{}
		}
	}

	var missing []string
	for route := range routerRoutes {
		if !strings.HasPrefix(route, "GET /api/") &&
			!strings.HasPrefix(route, "POST /api/") &&
			!strings.HasPrefix(route, "PUT /api/") &&
			!strings.HasPrefix(route, "PATCH /api/") &&
			!strings.HasPrefix(route, "DELETE /api/") &&
			!strings.HasPrefix(route, "GET /ws/") {
			continue
		}
		if _, ok := definedRoutes[route]; !ok {
			missing = append(missing, route)
		}
	}

	var extra []string
	for route := range definedRoutes {
		if strings.Contains(route, " /mcp") || strings.HasPrefix(route, "GET /files/") || route == "GET /health" {
			continue
		}
		if _, ok := routerRoutes[route]; !ok {
			extra = append(extra, route)
		}
	}

	slices.Sort(missing)
	slices.Sort(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("surface route drift\nmissing=%v\nextra=%v", missing, extra)
	}
}
