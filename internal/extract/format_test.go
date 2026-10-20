package extract

import (
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// line makes a test line at a height, from x0 to x1, in one font size.
func line(text string, y, x0, x1, size float64) Line {
	box := pdf.Rect{X0: x0, Y0: y, X1: x1, Y1: y + size}
	return Line{Text: text, Box: box, Size: size, PageH: 792, Spans: []pdf.Span{{Text: text, Box: box, Size: size}}}
}

func TestFormatText(t *testing.T) {
	got := formatText([]Line{
		line("TEMP(PIN1): Battery temperature input", 100, 46, 300, 14),
		line("Connect TEMP to the NTC sensor of the battery. If the", 120, 46, 560, 10),
		line("voltage is out of range, charging stops.", 133, 46, 300, 10),
		line("Charging is otherwise normal.", 146, 46, 200, 10),
		line("", 170, 46, 52, 10),
		line("Standby current 40uA", 170, 60, 200, 10),
		line("• Soft-start", 183, 46, 150, 10),
		line("Note: the pin floats", 196, 46, 160, 10),
	}, 10, nil)
	want := "### TEMP(PIN1): Battery temperature input\n\n" +
		"Connect TEMP to the NTC sensor of the battery. If the voltage is out of range, charging stops.\n\n" +
		"Charging is otherwise normal.\n\n" +
		"• Standby current 40uA\n• Soft-start\n\n" +
		"Note: the pin floats"
	if got != want {
		t.Errorf("got\n%s\n\nwant\n%s", got, want)
	}
}

func TestFormatTextChinese(t *testing.T) {
	got := formatText([]Line{
		line("当充电电流降至设定值的1/10时，充电", 100, 46, 560, 10),
		line("过程结束。", 113, 46, 120, 10),
		line("如果TEMP管脚的电", 126, 46, 120, 10),
		line("压过低，则充电被终止。", 142, 46, 300, 10),
	}, 10, nil)
	want := "当充电电流降至设定值的1/10时，充电过程结束。\n\n如果TEMP管脚的电压过低，则充电被终止。"
	if got != want {
		t.Errorf("got\n%s\n\nwant\n%s", got, want)
	}
}

func TestRunningKey(t *testing.T) {
	if runningKey("Submit Document Feedback 13") != runningKey("10 Submit Document Feedback") {
		t.Error("page numbers should not matter")
	}
}
