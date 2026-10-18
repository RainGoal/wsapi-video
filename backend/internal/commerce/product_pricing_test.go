package commerce

import "testing"

func pricedVideoFixture(unit string) Product {
	return Product{
		ID: "priced-video", Name: "Video", Kind: "video", Model: "fixture", Credits: 7,
		Capabilities: Capabilities{Durations: []int{5, 12}, Resolutions: []string{"720p", "1080p", "4k"}},
		Pricing:      &ProductPricing{BillingUnit: unit, ResolutionCredits: map[string]int64{"1080p": 13}},
	}
}

func TestProductPriceLegacyFixedCredits(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		p := Product{Kind: kind, Credits: 19}
		credits, breakdown, err := calculateProductPrice(p, QuoteInput{Duration: 12})
		if err != nil || credits != 19 || breakdown != nil {
			t.Fatalf("legacy %s price changed: %d %+v %v", kind, credits, breakdown, err)
		}
	}
}

func TestProductPriceUsesRequestedUnitsAndResolution(t *testing.T) {
	for _, tc := range []struct {
		unit, resolution string
		duration         int
		unitCredits      int64
		quantity         int64
		want             int64
	}{
		{"per_video", "720p", 5, 7, 1, 7},
		{"per_video", "1080p", 12, 13, 1, 13},
		{"per_second", "720p", 5, 7, 5, 35},
		{"per_second", "1080p", 12, 13, 12, 156},
		{"per_second", "4k", 12, 7, 12, 84},
		{"per_second", " 1080P ", 5, 13, 5, 65},
	} {
		t.Run(tc.unit+"/"+tc.resolution, func(t *testing.T) {
			p := pricedVideoFixture(tc.unit)
			p.Pricing.ResolutionCredits = map[string]int64{" 1080P ": 13}
			credits, detail, err := calculateProductPrice(p, QuoteInput{Duration: tc.duration, Resolution: tc.resolution})
			if err != nil || detail == nil {
				t.Fatal("valid price was rejected", err)
			}
			if credits != tc.want || detail.TotalCredits != credits || detail.BillingUnit != tc.unit || detail.UnitCredits != tc.unitCredits || detail.Quantity != tc.quantity || detail.Resolution != resolutionKey(tc.resolution) {
				t.Fatalf("incorrect quoted retail units: total=%d detail=%+v", credits, detail)
			}
		})
	}
}

func TestProductPriceUsesNormalizedInputDefaults(t *testing.T) {
	p := pricedVideoFixture("per_second")
	in := QuoteInput{ProjectID: "project", ProductID: p.ID, Prompt: "test"}
	if err := validateInput(&in, p); err != nil {
		t.Fatal(err)
	}
	credits, detail, err := calculateProductPrice(p, in)
	if err != nil || in.Duration != 5 || in.Resolution != "720p" || credits != 35 || detail.Quantity != 5 {
		t.Fatalf("normalized defaults were not used: input=%+v price=%d %v", in, credits, err)
	}
	in.Resolution = " 1080P "
	if err := validateInput(&in, p); err != nil || in.Resolution != "1080p" {
		t.Fatalf("resolution was not normalized to its verified capability: %+v %v", in, err)
	}
}

func TestProductPricingRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Product)
	}{
		{"image", func(p *Product) { p.Kind = "image" }},
		{"missing-unit", func(p *Product) { p.Pricing.BillingUnit = "" }},
		{"unknown-unit", func(p *Product) { p.Pricing.BillingUnit = "per_minute" }},
		{"missing-durations", func(p *Product) { p.Capabilities.Durations = nil }},
		{"zero-duration", func(p *Product) { p.Capabilities.Durations = []int{0} }},
		{"long-duration", func(p *Product) { p.Capabilities.Durations = []int{601} }},
		{"zero-default", func(p *Product) { p.Credits = 0 }},
		{"negative-default", func(p *Product) { p.Credits = -1 }},
		{"high-default", func(p *Product) { p.Credits = maxProductCredits + 1 }},
		{"default-total-overflow", func(p *Product) { p.Credits = maxProductCredits }},
		{"zero-override", func(p *Product) { p.Pricing.ResolutionCredits["1080p"] = 0 }},
		{"negative-override", func(p *Product) { p.Pricing.ResolutionCredits["1080p"] = -1 }},
		{"high-override", func(p *Product) { p.Pricing.ResolutionCredits["1080p"] = maxProductCredits + 1 }},
		{"override-total-overflow", func(p *Product) { p.Pricing.ResolutionCredits["1080p"] = maxProductCredits }},
		{"unsupported-resolution", func(p *Product) { p.Pricing.ResolutionCredits["8k"] = 20 }},
		{"blank-resolution", func(p *Product) { p.Pricing.ResolutionCredits[" "] = 20 }},
		{"duplicate-resolution", func(p *Product) { p.Pricing.ResolutionCredits[" 1080P "] = 20 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := pricedVideoFixture("per_second")
			tc.mutate(&p)
			if err := validateProductPricing(p); err == nil {
				t.Fatal("invalid pricing configuration was accepted")
			}
			if validateProduct(p) {
				t.Fatal("admin validation accepted invalid pricing")
			}
			if _, _, err := calculateProductPrice(p, QuoteInput{Duration: 5, Resolution: "1080p"}); err == nil {
				t.Fatal("quote accepted invalid pricing configuration")
			}
		})
	}
}

func TestProductPriceRejectsInvalidSelectedSpecifications(t *testing.T) {
	for _, in := range []QuoteInput{
		{Duration: 0, Resolution: "720p"},
		{Duration: -1, Resolution: "720p"},
		{Duration: 6, Resolution: "720p"},
		{Duration: int(^uint(0) >> 1), Resolution: "720p"},
		{Duration: 5, Resolution: "8k"},
		{Duration: 5},
	} {
		if _, _, err := calculateProductPrice(pricedVideoFixture("per_second"), in); err == nil {
			t.Fatalf("invalid frozen specification was accepted: %+v", in)
		}
	}
}

func TestProductPriceCreditBoundaries(t *testing.T) {
	for _, unit := range []string{"per_video", "per_second"} {
		p := pricedVideoFixture(unit)
		p.Capabilities.Durations = []int{5}
		p.Credits = maxProductCredits
		p.Pricing.ResolutionCredits = nil
		if unit == "per_second" {
			p.Credits /= 5
		}
		if !validateProduct(p) {
			t.Fatal("upper-bound valid product was rejected", unit)
		}
		credits, detail, err := calculateProductPrice(p, QuoteInput{Duration: 5, Resolution: "720p"})
		if err != nil || credits != maxProductCredits || detail.TotalCredits != maxProductCredits {
			t.Fatal("exact credit limit was rejected", unit, credits, detail, err)
		}
		p.Credits++
		if _, _, err := calculateProductPrice(p, QuoteInput{Duration: 5, Resolution: "720p"}); err == nil {
			t.Fatal("above-limit retail total was accepted", unit)
		}
	}
	p := pricedVideoFixture("per_second")
	p.Credits = 1
	p.Capabilities.Durations = []int{1}
	p.Pricing.ResolutionCredits = nil
	credits, _, err := calculateProductPrice(p, QuoteInput{Duration: 1, Resolution: "720p"})
	if err != nil || credits != 1 {
		t.Fatal("minimum price was rejected", credits, err)
	}
}
