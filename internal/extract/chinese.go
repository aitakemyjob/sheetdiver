package extract

import (
	"regexp"
	"strings"
)

// zhHeadings are Chinese section headings with the English heading each
// stands for, so a Chinese datasheet gets its sections (and the figures
// under them) without translation.
var zhHeadings = map[string]string{
	"概述": "Description", "产品概述": "Description", "描述": "Description",
	"特点": "Features", "产品特点": "Features", "特性": "Features",
	"应用": "Applications", "应用领域": "Applications", "应用范围": "Applications",
	"典型应用电路": "Typical application circuit", "典型应用": "Typical application", "典型应用电路图": "Typical application circuit",
	"管脚": "Pin configuration", "引脚": "Pin configuration", "管脚排列": "Pin configuration", "引脚排列": "Pin configuration",
	"管脚定义": "Pin configuration", "引脚定义": "Pin configuration", "管脚分布": "Pin configuration", "引脚分布": "Pin configuration",
	"管脚描述": "Pin description", "引脚描述": "Pin description", "管脚说明": "Pin description", "引脚说明": "Pin description",
	"管脚功能": "Pin functions", "引脚功能": "Pin functions", "引脚功能说明": "Pin functions", "管脚功能说明": "Pin functions",
	"定购信息": "Ordering information", "订购信息": "Ordering information", "订货信息": "Ordering information",
	"极限参数": "Absolute maximum ratings", "绝对最大额定值": "Absolute maximum ratings", "最大额定值": "Absolute maximum ratings", "极限额定值": "Absolute maximum ratings",
	"推荐工作条件": "Recommended operating conditions", "推荐工作范围": "Recommended operating conditions",
	"电气特性": "Electrical characteristics", "电特性": "Electrical characteristics", "电气参数": "Electrical characteristics",
	"电参数": "Electrical characteristics", "电性参数": "Electrical characteristics", "电气特性参数": "Electrical characteristics",
	"热特性": "Thermal information", "热阻": "Thermal information",
	"典型特性": "Typical characteristics", "典型特性曲线": "Typical characteristics", "典型性能特征": "Typical characteristics",
	"内部框图": "Functional block diagram", "功能框图": "Functional block diagram", "原理框图": "Functional block diagram", "框图": "Block diagram",
	"工作原理": "Detailed description", "功能描述": "Detailed description",
	"应用说明": "Application information", "应用信息": "Application information",
	"封装信息": "Package information", "封装尺寸": "Package dimensions", "封装外形尺寸": "Package dimensions", "封装外形": "Package dimensions",
}

// zhHeadingRe splits a Chinese heading into an optional number ("3.",
// "二、"), the heading and an optional note ("（注 1）") or colon.
var zhHeadingRe = regexp.MustCompile(`^(?:\d+(?:\.\d+)*\.?\s*|[一二三四五六七八九十]+[、.．]\s*)?(\p{Han}+?)\s*(?:[（(]\s*注[^)）]*[)）])?\s*[:：]?$`)

// zhHeading returns the English heading a Chinese heading line stands
// for, or "".
func zhHeading(text string) string {
	m := zhHeadingRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return ""
	}
	return zhHeadings[m[1]]
}

// isPlainHeading reports whether a line is an unnumbered heading worth
// recognising, in English or Chinese.
func isPlainHeading(text string) bool {
	t := strings.TrimSpace(text)
	return plainHeadings.MatchString(t) || zhHeading(t) != ""
}
