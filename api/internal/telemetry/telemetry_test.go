package telemetry

import (
	"context"
	"testing"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

func TestInitIsNoopWithoutEndpoint(t *testing.T) {
	shutdown, err := Init(context.Background(), config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitWithEndpointBuildsAProvider(t *testing.T) {
	for _, proto := range []string{"http/protobuf", "grpc"} {
		shutdown, err := Init(context.Background(), config.Config{OTelEndpoint: "http://127.0.0.1:1", OTelProtocol: proto})
		if err != nil {
			t.Fatalf("%s: %v", proto, err)
		}
		_ = shutdown(context.Background()) // exporter is lazy; nothing is listening
	}
}
