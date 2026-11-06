package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/khangpworking/braveduck-cli/internal/service"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

func TestCrawlScheduleAdvertisesModeMinimum(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hosted  bool
		minimum int
	}{
		{"local", false, 15},
		{"hosted", true, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			svc := service.New(st)
			if tc.hosted {
				svc.SetHosted(service.HostedInfo{User: "alice"})
			}
			ct, transport := sdk.NewInMemoryTransports()
			ss, err := NewServer(svc).Connect(ctx, transport, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ss.Close() })
			cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cs.Close() })
			listed, err := cs.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var description string
			for _, tool := range listed.Tools {
				if tool.Name == "set_crawl_schedule" {
					description = tool.Description
				}
			}
			// This is the numeric range delivered to the agent in tools/list,
			// paired below with calls to the same advertised tool.
			if want := fmt.Sprintf("%d-1440", tc.minimum); !strings.Contains(description, want) {
				t.Errorf("tool description = %q, want the mode's range %s", description, want)
			}
			for _, edge := range []struct {
				source    string
				minutes   int
				wantError bool
			}{
				{"vietnamworks", tc.minimum, false},
				{"vietnamworks", tc.minimum - 1, true},
				{"vietnamworks", 0, false},
				{"external-test", 5, false},
			} {
				result := call(t, cs, "set_crawl_schedule", map[string]any{"source": edge.source, "every_minutes": edge.minutes}, nil)
				if result.IsError != edge.wantError {
					t.Errorf("%s at %d minutes: isError=%v, output=%s", edge.source, edge.minutes, result.IsError, text(result))
				}
			}
		})
	}
}
