package commerce

import (
	"errors"
	"strings"
)

const maxProductCredits int64 = 1_000_000_000

func resolutionKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateProductPricing(p Product) error {
	if p.Credits < 1 || p.Credits > maxProductCredits {
		return errors.New("默认单位积分须为 1–1,000,000,000 的整数")
	}
	if p.Pricing == nil {
		return nil
	}
	if p.Kind != "video" || (p.Pricing.BillingUnit != "per_video" && p.Pricing.BillingUnit != "per_second") {
		return errors.New("仅视频商品支持按次或按请求秒数计价")
	}
	quantity := int64(1)
	if p.Pricing.BillingUnit == "per_second" {
		if len(p.Capabilities.Durations) == 0 {
			return errors.New("按秒计价必须配置可选视频时长")
		}
		for _, duration := range p.Capabilities.Durations {
			if duration < 1 || duration > 600 {
				return errors.New("可选视频时长须为 1–600 秒")
			}
			if int64(duration) > quantity {
				quantity = int64(duration)
			}
		}
	}
	if quantity > maxProductCredits/p.Credits {
		return errors.New("默认单位积分与可选时长的总价超过积分上限")
	}
	seen := make(map[string]bool, len(p.Pricing.ResolutionCredits))
	for resolution, credits := range p.Pricing.ResolutionCredits {
		key := resolutionKey(resolution)
		if key == "" || len(resolution) > 100 || seen[key] {
			return errors.New("分辨率定价的规格不能为空或重复")
		}
		seen[key] = true
		supported := false
		for _, allowed := range p.Capabilities.Resolutions {
			if key == resolutionKey(allowed) {
				supported = true
				break
			}
		}
		if !supported {
			return errors.New("分辨率定价必须属于商品能力中的规格")
		}
		if credits < 1 || credits > maxProductCredits || quantity > maxProductCredits/credits {
			return errors.New("分辨率单位积分及所有可选时长的总价须为 1–1,000,000,000")
		}
	}
	return nil
}

// The input must already have its default duration and resolution selected.
// A missing pricing configuration preserves the original fixed-price contract.
func calculateProductPrice(p Product, in QuoteInput) (int64, *PriceBreakdown, error) {
	if err := validateProductPricing(p); err != nil {
		return 0, nil, err
	}
	if p.Pricing == nil {
		return p.Credits, nil, nil
	}
	resolution := resolutionKey(in.Resolution)
	if len(p.Capabilities.Resolutions) > 0 || resolution != "" {
		supported := false
		for _, allowed := range p.Capabilities.Resolutions {
			if resolution == resolutionKey(allowed) {
				supported = true
				break
			}
		}
		if !supported {
			return 0, nil, errors.New("生成分辨率不属于已验证的商品规格")
		}
	}
	unitCredits := p.Credits
	for key, credits := range p.Pricing.ResolutionCredits {
		if resolutionKey(key) == resolution {
			unitCredits = credits
			break
		}
	}
	quantity := int64(1)
	if p.Pricing.BillingUnit == "per_second" {
		if in.Duration < 1 || !includes(p.Capabilities.Durations, in.Duration) {
			return 0, nil, errors.New("按秒计价须选择已验证的正整数请求时长")
		}
		quantity = int64(in.Duration)
	}
	if quantity > maxProductCredits/unitCredits {
		return 0, nil, errors.New("生成总积分超过 1,000,000,000 上限")
	}
	total := unitCredits * quantity
	return total, &PriceBreakdown{
		BillingUnit: p.Pricing.BillingUnit, UnitCredits: unitCredits,
		Quantity: quantity, Resolution: resolution, TotalCredits: total,
	}, nil
}
