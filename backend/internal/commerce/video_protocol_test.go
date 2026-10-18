package commerce

import "testing"

func TestVideoProductRejectsImageSpecifications(t *testing.T) {
	for _, field := range []string{"resolution", "size", "quality"} {
		t.Run(field, func(t *testing.T) {
			p := Product{ID: "video", Name: "Video", Model: "fixture", Kind: "video", Credits: 10}
			switch field {
			case "resolution":
				p.Capabilities.Resolutions = []string{"720p"}
			case "size":
				p.Capabilities.Sizes = []string{"720p"}
			case "quality":
				p.Capabilities.Qualities = []string{"high"}
			}
			if got := validateProduct(p); got != (field == "resolution") {
				t.Fatalf("video %s product valid=%v", field, got)
			}
		})
	}
}

func TestVideoProtocolRejectsImageSpecifications(t *testing.T) {
	for _, field := range []string{"size", "quality", "legacy_size", "legacy_quality"} {
		t.Run(field, func(t *testing.T) {
			p := Product{Kind: "video", Capabilities: Capabilities{Resolutions: []string{"720p"}, Sizes: []string{}, Qualities: []string{}}}
			in := QuoteInput{ProjectID: "project", ProductID: "product", Prompt: "test"}
			switch field {
			case "size":
				in.Size = "1024x1024"
				p.Capabilities.Sizes = []string{in.Size}
			case "quality":
				in.Quality = "high"
				p.Capabilities.Qualities = []string{in.Quality}
			case "legacy_size":
				p.Capabilities.Sizes = []string{"720p"}
			case "legacy_quality":
				p.Capabilities.Qualities = []string{"high"}
			}
			if err := validateInput(&in, p); err == nil {
				t.Fatal("unsupported video image specifications were accepted")
			}
		})
	}
}

func TestVideoProtocolPreservesSupportedMediaSpecifications(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			p := Product{Kind: kind}
			if kind == "image" {
				p.Capabilities.Sizes = []string{"1024x1024"}
				p.Capabilities.Qualities = []string{"high"}
			} else {
				p.Capabilities.Resolutions = []string{"720p"}
			}
			in := QuoteInput{ProjectID: "project", ProductID: "product", Prompt: "test"}
			if err := validateInput(&in, p); err != nil {
				t.Fatal(err)
			}
			if kind == "image" && (in.Size != "1024x1024" || in.Quality != "high") {
				t.Fatalf("image defaults were changed: %#v", in)
			}
			if kind == "video" && (in.Resolution != "720p" || in.Size != "" || in.Quality != "") {
				t.Fatalf("video defaults were changed: %#v", in)
			}
		})
	}
}
