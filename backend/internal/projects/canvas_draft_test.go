package projects

import (
	"encoding/json"
	"testing"
)

func TestCanvasDraftAssetReferences(t *testing.T) {
	graph := json.RawMessage(`{"nodes":[{"id":"node","metadata":{"generationDraft":{"referenceIds":["reference"],"startFrameId":"first","endFrameId":"last","connectedAssetIds":["connected"]},"generationInputSourceAssetIds":["baseline"],"generationSourceNodeId":"source-node","productId":"product"}}],"edges":[]}`)
	ids, err := graphReferences(graph)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"reference", "first", "last", "connected", "baseline"} {
		if _, ok := ids[id]; !ok {
			t.Fatalf("draft asset %q was not registered for ownership and deletion protection", id)
		}
	}
	if len(ids) != 5 {
		t.Fatalf("node and product identifiers must not become asset references: %v", ids)
	}
}
