package coding

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/sky-valley/pi/agent"
	"github.com/sky-valley/pi/ai"
	"github.com/sky-valley/pi/ai/providers"
)

// grayPNGBase64 builds an 8-bit grayscale PNG of the given size, base64-encoded
// the way a tool would hand it back.
func grayPNGBase64(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return encodeBase64(buf.Bytes())
}

func pngDimensions(t *testing.T, data string) (int, int) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func TestNormalizeToolResultImagesNoImages(t *testing.T) {
	content := ai.ContentList{ai.TextContent{Text: "no images here"}}
	out, changed := normalizeToolResultImages(content)
	if changed {
		t.Fatal("content without images must be reported unchanged")
	}
	if len(out) != 1 {
		t.Fatalf("content rewritten: %#v", out)
	}
}

func TestNormalizeToolResultImagesWithinLimits(t *testing.T) {
	small := grayPNGBase64(t, 8, 8)
	content := ai.ContentList{
		ai.TextContent{Text: "screenshot"},
		ai.ImageContent{Data: small, MimeType: "image/png"},
	}
	out, changed := normalizeToolResultImages(content)
	if changed {
		t.Fatal("image within limits must be reported unchanged")
	}
	if img, _ := out[1].(ai.ImageContent); img.Data != small {
		t.Fatal("image data rewritten")
	}
}

func TestNormalizeToolResultImagesResizesOversized(t *testing.T) {
	content := ai.ContentList{ai.ImageContent{Data: grayPNGBase64(t, 2400, 4800), MimeType: "image/png"}}

	out, changed := normalizeToolResultImages(content)
	if !changed {
		t.Fatal("oversized image must be normalized")
	}
	if len(out) != 2 {
		t.Fatalf("expected image + hint block, got %#v", out)
	}
	img, ok := out[0].(ai.ImageContent)
	if !ok {
		t.Fatalf("first block is not an image: %#v", out[0])
	}
	w, h := pngDimensions(t, img.Data)
	if w > imgMaxWidth || h > imgMaxHeight {
		t.Fatalf("image not downscaled: %dx%d", w, h)
	}
	note, ok := out[1].(ai.TextContent)
	if !ok {
		t.Fatalf("second block is not text: %#v", out[1])
	}
	if !strings.Contains(note.Text, "original 2400x4800") {
		t.Fatalf("hint missing original dimensions: %q", note.Text)
	}
}

func TestNormalizeToolResultImagesConvertsUnsupportedFormat(t *testing.T) {
	content := ai.ContentList{ai.ImageContent{Data: encodeBase64(tinyBMP1x1Red24bpp()), MimeType: "image/bmp"}}

	out, changed := normalizeToolResultImages(content)
	if !changed {
		t.Fatal("unsupported format must be converted")
	}
	img, ok := out[0].(ai.ImageContent)
	if !ok || img.MimeType != "image/png" {
		t.Fatalf("expected PNG image block, got %#v", out[0])
	}
	note, ok := out[1].(ai.TextContent)
	if !ok || note.Text != "[Image converted from image/bmp to image/png.]" {
		t.Fatalf("unexpected conversion hint: %#v", out[1])
	}
}

func TestNormalizeToolResultImagesKeepsUndecodable(t *testing.T) {
	content := ai.ContentList{ai.ImageContent{Data: "bm90LWFuLWltYWdl", MimeType: "image/png"}}
	out, changed := normalizeToolResultImages(content)
	if changed {
		t.Fatal("undecodable image must be kept as-is, not dropped")
	}
	if img, _ := out[0].(ai.ImageContent); img.Data != "bm90LWFuLWltYWdl" {
		t.Fatalf("undecodable image was rewritten: %#v", out[0])
	}
}

func TestNormalizeToolResultImagesPreservesSurroundingText(t *testing.T) {
	content := ai.ContentList{
		ai.TextContent{Text: "before"},
		ai.ImageContent{Data: grayPNGBase64(t, 2400, 100), MimeType: "image/png"},
		ai.TextContent{Text: "after"},
	}

	out, changed := normalizeToolResultImages(content)
	if !changed {
		t.Fatal("oversized image must be normalized")
	}
	var kinds []string
	for _, block := range out {
		switch block.(type) {
		case ai.TextContent:
			kinds = append(kinds, "text")
		case ai.ImageContent:
			kinds = append(kinds, "image")
		default:
			kinds = append(kinds, "other")
		}
	}
	if strings.Join(kinds, ",") != "text,image,text,text" {
		t.Fatalf("unexpected block order: %v", kinds)
	}
	if first, _ := out[0].(ai.TextContent); first.Text != "before" {
		t.Fatalf("leading text changed: %#v", out[0])
	}
	if last, _ := out[3].(ai.TextContent); last.Text != "after" {
		t.Fatalf("trailing text changed: %#v", out[3])
	}
}

// screenshotTool stands in for custom SDK tools, MCP bridges, or screenshot
// tools that return images they produced themselves.
func screenshotTool(data string) agent.AgentTool {
	return agent.AgentTool{
		Name:        "screenshot",
		Label:       "Screenshot",
		Description: "Return an oversized screenshot",
		Parameters:  ai.Object(),
		Execute: func(ctx context.Context, id string, params map[string]any, onUpdate agent.ToolUpdateFunc) (agent.AgentToolResult, error) {
			return agent.AgentToolResult{Content: ai.ContentList{
				ai.TextContent{Text: "captured"},
				ai.ImageContent{Data: data, MimeType: "image/png"},
			}, Details: map[string]any{}}, nil
		},
	}
}

func runScreenshotSession(t *testing.T, opts SessionOptions) []ai.ImageContent {
	t.Helper()
	reg := providers.RegisterFauxProvider(providers.RegisterFauxProviderOptions{})
	defer reg.Unregister()
	reg.SetResponses([]providers.FauxResponseStep{
		providers.FauxStatic(providers.FauxAssistantMessage(ai.ContentList{
			providers.FauxToolCall("screenshot", map[string]any{}, "c1"),
		}, ai.StopToolUse)),
		providers.FauxStatic(providers.FauxAssistantMessage(ai.ContentList{ai.TextContent{Text: "done"}}, ai.StopStop)),
	})
	opts.Model = reg.GetModel()
	opts.Cwd = t.TempDir()

	res, err := NewSession(opts).Run(context.Background(), "take a screenshot")
	if err != nil {
		t.Fatal(err)
	}
	var images []ai.ImageContent
	for _, m := range res.Messages {
		tr, ok := m.(ai.ToolResultMessage)
		if !ok {
			continue
		}
		for _, block := range tr.Content {
			if img, ok := block.(ai.ImageContent); ok {
				images = append(images, img)
			}
		}
	}
	return images
}

// TestSessionResizesToolResultImages locks pi's fix: images returned by tools
// are resized before they enter session history.
func TestSessionResizesToolResultImages(t *testing.T) {
	oversized := grayPNGBase64(t, 2400, 4800)
	images := runScreenshotSession(t, SessionOptions{CustomTools: []agent.AgentTool{screenshotTool(oversized)}})

	if len(images) != 1 {
		t.Fatalf("expected one image in history, got %d", len(images))
	}
	w, h := pngDimensions(t, images[0].Data)
	if w > imgMaxWidth || h > imgMaxHeight {
		t.Fatalf("tool result image not resized: %dx%d", w, h)
	}
}

// TestSessionResizesImagesInjectedByAfterToolCall locks that normalization runs
// AFTER the hook, so images the hook injects are normalized too.
func TestSessionResizesImagesInjectedByAfterToolCall(t *testing.T) {
	oversized := grayPNGBase64(t, 2400, 4800)
	small := grayPNGBase64(t, 8, 8)
	images := runScreenshotSession(t, SessionOptions{
		CustomTools: []agent.AgentTool{screenshotTool(small)},
		AfterToolCall: func(ctx context.Context, c agent.AfterToolCallContext) *agent.AfterToolCallResult {
			return &agent.AfterToolCallResult{
				Content:    ai.ContentList{ai.ImageContent{Data: oversized, MimeType: "image/png"}},
				HasContent: true,
			}
		},
	})

	if len(images) != 1 {
		t.Fatalf("expected one image in history, got %d", len(images))
	}
	w, h := pngDimensions(t, images[0].Data)
	if w > imgMaxWidth || h > imgMaxHeight {
		t.Fatalf("hook-injected image not resized: %dx%d", w, h)
	}
}

// TestNormalizeToolResultImagesLenientBase64 locks the decode of tool-returned
// image payloads to Node's Buffer.from(x, "base64"). Go's StdEncoding happens to
// skip \r and \n, but it rejects the base64url alphabet and spaces outright, so
// such a payload failed to decode and the oversized image was passed through
// UNRESIZED, partially defeating the resize of tool-returned images.
func TestNormalizeToolResultImagesLenientBase64(t *testing.T) {
	oversized := grayPNGBase64(t, 3000, 100)

	// base64url: the alphabet a tool emitting URL-safe base64 would produce.
	urlSafe := strings.NewReplacer("+", "-", "/", "_", "=", "").Replace(oversized)
	// Space-separated groups: whitespace Node ignores and Go rejects.
	var spaced strings.Builder
	for i := 0; i < len(oversized); i += 76 {
		end := min(i+76, len(oversized))
		spaced.WriteString(oversized[i:end])
		spaced.WriteByte(' ')
	}

	for name, payload := range map[string]string{"base64url": urlSafe, "spaces": spaced.String()} {
		t.Run(name, func(t *testing.T) {
			content := ai.ContentList{ai.ImageContent{Data: payload, MimeType: "image/png"}}
			out, changed := normalizeToolResultImages(content)
			if !changed {
				t.Fatal("oversized image was not resized; the decode is stricter than Node's")
			}
			img, ok := out[0].(ai.ImageContent)
			if !ok {
				t.Fatalf("expected an image block, got %T", out[0])
			}
			if w, _ := pngDimensions(t, img.Data); w > imgMaxWidth {
				t.Fatalf("resized width = %d, want <= %d", w, imgMaxWidth)
			}
		})
	}
}
