package project

import "strings"

// VisionImageSectionMarker is the image-analysis section title (consistent with what AppendVisionImageAnalysisIfReady injects).
const VisionImageSectionMarker = "## Image analysis"

// VisionImageAnalysisSection is the image-analysis hint shared by single/multi-agent mode (analyze_image; only a text summary is kept in context).
func VisionImageAnalysisSection() string {
	var b strings.Builder
	b.WriteString(VisionImageSectionMarker)
	b.WriteString("\n\n")
	b.WriteString("- When you encounter an image file (screenshot, captcha, login page, report figure), if the analyze_image tool exists, pass the file path on the server to analyze it.\n")
	b.WriteString("- Do not use read_file on binary images expecting to understand the content; the \"📎 xxx.png: /path\" in a user message is the path you can pass to analyze_image.\n")
	b.WriteString("- Captchas: if one has been saved as a local image from the page or an endpoint (e.g. captcha.png), use analyze_image with a question of \"only output the captcha characters\"; if recognition fails, refresh the captcha, save it again, and retry; do not expect a single recognition to succeed for complex slider/behavioral captchas.\n")
	b.WriteString("- When delegating to a sub-agent, if the subtask involves reading a captcha/screenshot, state the image path and expected output format in the task description.\n")
	return b.String()
}

// AppendVisionImageAnalysisIfReady appends the image-analysis hint only when vision.enabled and a model are configured.
func AppendVisionImageAnalysisIfReady(base string, visionReady bool) string {
	if !visionReady {
		return base
	}
	return AppendSystemPromptBlock(base, VisionImageAnalysisSection())
}
