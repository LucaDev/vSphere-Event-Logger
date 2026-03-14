package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/vim25/types"
)

// flattenMap recursively collapses nested map[string]interface{} into a single-level map.
// It detects standard vSphere EventArguments which contain {"name": "...", "vm": {"type": "VirtualMachine", "value": "vm-123"}}
// and flattens them as:
//
//	prefix = "name"
//	prefix_id = "vm-123"
func flattenMap(prefix string, src map[string]interface{}, dest map[string]interface{}) {
	// Special vSphere v25 EventArgument heuristic:
	// If src has "name" and exactly one other key which is a map containing "value" and "type".
	if name, ok := src["name"].(string); ok && prefix != "" {
		dest[prefix] = name

		for k, v := range src {
			if k == "name" {
				continue
			}
			if child, isMap := v.(map[string]interface{}); isMap {
				if val, hasValue := child["value"].(string); hasValue {
					dest[prefix+"_id"] = val
				}
			}
		}
		return
	}

	for k, v := range src {
		newKey := k
		if prefix != "" {
			newKey = prefix + "_" + k
		}
		if childMap, ok := v.(map[string]interface{}); ok {
			flattenMap(newKey, childMap, dest)
		} else {
			dest[newKey] = v
		}
	}
}

func main() {
	// Set up structured JSON logging to stdout
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Define flags
	redactUsernamePtr := flag.Bool("redact-username", false, "Hash the username in the output (can also be set with REDACT_USERNAME=true)")
	flag.Parse()

	// Parse environment variables
	if strings.ToLower(os.Getenv("REDACT_USERNAME")) == "true" {
		*redactUsernamePtr = true
	}

	insecure := strings.ToLower(os.Getenv("GOVMOMI_INSECURE")) == "true"

	envURL := os.Getenv("GOVMOMI_URL")
	if envURL == "" {
		slog.Error("GOVMOMI_URL not set")
		os.Exit(1)
	}

	u, err := url.Parse(envURL)
	if err != nil {
		slog.Error("Failed to parse GOVMOMI_URL", "error", err)
		os.Exit(1)
	}

	username := os.Getenv("GOVMOMI_USERNAME")
	password := os.Getenv("GOVMOMI_PASSWORD")
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}

	slog.Info("Connecting to vSphere", "url", u.Host, "insecure", insecure, "redactUsername", *redactUsernamePtr)

	// Create client
	client, err := govmomi.NewClient(ctx, u, insecure)
	if err != nil {
		slog.Error("Failed to connect to vSphere", "error", err)
		os.Exit(1)
	}
	defer client.Logout(ctx) // Ensure we logout

	// Load custom CA certificates if specified
	tlsCAs := os.Getenv("GOVMOMI_TLS_CA_CERTS")
	if tlsCAs != "" {
		slog.Info("Loading custom TLS CA certificates", "paths", tlsCAs)
		if err := client.Client.SetRootCAs(tlsCAs); err != nil {
			slog.Error("Failed to set custom root CAs", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("Successfully connected to vSphere")

	manager := event.NewManager(client.Client)

	// Create channel for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		slog.Info("Received termination signal, shutting down")
		cancel()
	}()

	slog.Info("Starting event listener")

	// Start listening to events.
	// Parameters: ctx, objects (nil for root namespace), pageSize (10 is reasonable), tail=true, force=false, callback function.
	err = manager.Events(ctx, []types.ManagedObjectReference{client.ServiceContent.RootFolder}, 10, true, false, func(obj types.ManagedObjectReference, events []types.BaseEvent) error {
		for _, e := range events {
			eventType := reflect.TypeOf(e).Elem().Name()

			b, err := json.Marshal(e)
			if err != nil {
				slog.Error("Failed to marshal event", "error", err)
				continue
			}

			// Extract the severity/category of the event
			severity, err := manager.EventCategory(ctx, e)
			if err != nil {
				slog.Warn("Failed to get event category", "error", err)
			}

			var rawMap map[string]interface{}
			if err := json.Unmarshal(b, &rawMap); err != nil {
				slog.Error("Failed to unmarshal event to map", "error", err)
				continue
			}

			flatEvent := make(map[string]interface{})
			flattenMap("", rawMap, flatEvent)

			// replace createdTime with time for consistency across all log lines
			flatEvent["time"] = flatEvent["createdTime"]
			delete(flatEvent, "createdTime")

			// rename fullFormattedMessage to message if message is not set to anything else -> helps grafana visualization
			if flatEvent["fullFormattedMessage"] != nil && flatEvent["message"] == nil {
				flatEvent["message"] = flatEvent["fullFormattedMessage"]
				delete(flatEvent, "fullFormattedMessage")
			}

			// add eventType and level
			flatEvent["eventType"] = eventType
			if severity != "" {
				flatEvent["level"] = severity
			} else {
				flatEvent["level"] = "unknown"
			}

			// redact username if requested
			if *redactUsernamePtr {
				if uname, ok := flatEvent["userName"].(string); ok && uname != "" {
					hash := sha256.Sum256([]byte(uname))
					flatEvent["userName"] = hex.EncodeToString(hash[:])[:8] // Keep first 8 characters of hash
				}
			}

			// marshal flat event
			bFlat, err := json.Marshal(flatEvent)
			if err != nil {
				slog.Error("Failed to marshal flat event", "error", err)
				continue
			}

			fmt.Println(string(bFlat))
		}
		return nil
	})

	if err != nil {
		// If context was canceled it simply means we shut down cleanly
		if ctx.Err() == context.Canceled {
			slog.Info("Event listener stopped cleanly")
		} else {
			slog.Error("Event listener failed", "error", err)
			os.Exit(1)
		}
	}
}
