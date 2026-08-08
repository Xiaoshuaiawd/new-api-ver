package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	juice_setting "github.com/QuantumNous/new-api/setting/juice_fixer_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

const juiceContextKey = "juice_fixer_context"

var (
	juiceTriggerPattern     = regexp.MustCompile(`(?i)(?:\bjuice\b|j\s*u\s*i\s*c\s*e|果汁|果汁值|果汁数值|果汁數值)`)
	juiceNumberPattern      = regexp.MustCompile(`(?i)((?:\bjuice\b|j\s*u\s*i\s*c\s*e|果汁|果汁值|果汁数值|果汁數值)[^\d\r\n]{0,48})([-+]?\d+(?:\.\d+)?)`)
	standaloneNumberPattern = regexp.MustCompile(`^\s*[-+]?\d+(?:\.\d+)?\s*$`)
)

type JuiceContext struct {
	Triggered bool
}

type JuiceStreamTransformer struct {
	value   int
	pending string
	matched bool
}

func NewJuiceStreamTransformer(value int) *JuiceStreamTransformer {
	return &JuiceStreamTransformer{value: value}
}

func (t *JuiceStreamTransformer) Transform(text string) string {
	if t == nil || text == "" {
		return text
	}
	if t.matched {
		return text
	}
	t.pending += text
	if juiceNumberComplete(t.pending) {
		replaced, ok := ReplaceJuiceNumber(t.pending, t.value)
		if !ok {
			return ""
		}
		t.pending = ""
		t.matched = true
		return replaced
	}
	if len([]rune(t.pending)) > 256 {
		runes := []rune(t.pending)
		flushAt := len(runes) - 64
		out := string(runes[:flushAt])
		t.pending = string(runes[flushAt:])
		return out
	}
	return ""
}

func juiceNumberComplete(text string) bool {
	if standaloneNumberPattern.MatchString(text) {
		return false
	}
	match := juiceNumberPattern.FindStringSubmatchIndex(text)
	return match != nil && match[5] < len(text)
}

func (t *JuiceStreamTransformer) Flush() string {
	if t == nil || t.pending == "" {
		return ""
	}
	out, _ := ReplaceJuiceNumber(t.pending, t.value)
	t.pending = ""
	return out
}

func TransformChatStreamChunks(chunks []string, value int) []string {
	transformer := NewJuiceStreamTransformer(value)
	transformed := make([]string, len(chunks))
	lastContentChunk := -1
	lastContentChoice := -1
	for i, chunk := range chunks {
		transformed[i] = chunk
		var response dto.ChatCompletionsStreamResponse
		if err := UnmarshalJSONText(chunk, &response); err != nil {
			continue
		}
		for choiceIndex := range response.Choices {
			content := response.Choices[choiceIndex].Delta.GetContentString()
			if content == "" {
				continue
			}
			lastContentChunk, lastContentChoice = i, choiceIndex
			output := transformer.Transform(content)
			if output == content {
				continue
			}
			path := fmt.Sprintf("choices.%d.delta.content", choiceIndex)
			var err error
			if output == "" {
				transformed[i], err = sjson.Delete(transformed[i], path)
			} else {
				transformed[i], err = sjson.Set(transformed[i], path, output)
			}
			if err != nil {
				transformed[i] = chunk
			}
		}
	}
	flush := transformer.Flush()
	if flush == "" {
		return transformed
	}
	if lastContentChunk >= 0 {
		path := fmt.Sprintf("choices.%d.delta.content", lastContentChoice)
		updated, err := sjson.Set(transformed[lastContentChunk], path, flush)
		if err == nil {
			transformed[lastContentChunk] = updated
		}
	}
	return transformed
}

func TransformResponsesStreamChunks(chunks []string, value int) []string {
	transformer := NewJuiceStreamTransformer(value)
	transformed := make([]string, len(chunks))
	lastDeltaChunk := -1
	for i, chunk := range chunks {
		transformed[i] = chunk
		var response dto.ResponsesStreamResponse
		if err := UnmarshalJSONText(chunk, &response); err != nil || response.Type != "response.output_text.delta" {
			continue
		}
		lastDeltaChunk = i
		output := transformer.Transform(response.Delta)
		if output == response.Delta {
			continue
		}
		var err error
		if output == "" {
			transformed[i], err = sjson.Delete(transformed[i], "delta")
		} else {
			transformed[i], err = sjson.Set(transformed[i], "delta", output)
		}
		if err != nil {
			transformed[i] = chunk
		}
	}
	flush := transformer.Flush()
	if flush == "" {
		return transformed
	}
	if lastDeltaChunk >= 0 {
		updated, err := sjson.Set(transformed[lastDeltaChunk], "delta", flush)
		if err == nil {
			transformed[lastDeltaChunk] = updated
		}
	}
	return transformed
}

func UnmarshalJSONText(data string, target any) error {
	return common.UnmarshalJsonStr(data, target)
}

func BuildJuiceContext(request dto.Request) JuiceContext {
	var latestUser, systemText string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		for _, message := range req.Messages {
			text := message.StringContent()
			switch strings.ToLower(strings.TrimSpace(message.Role)) {
			case "system", "developer":
				systemText += "\n" + text
			case "user":
				latestUser = text
			}
		}
		if req.Prompt != nil {
			latestUser += "\n" + fmt.Sprint(req.Prompt)
		}
	case *dto.OpenAIResponsesRequest:
		systemText = string(req.Instructions)
		for _, input := range req.ParseInput() {
			latestUser += "\n" + input.Text
		}
		latestUser += "\n" + string(req.Prompt)
	}
	return JuiceContext{Triggered: juiceTriggerPattern.MatchString(normalizeJuiceText(latestUser)) || juiceTriggerPattern.MatchString(normalizeJuiceText(systemText))}
}

func SetJuiceContext(c *gin.Context, context JuiceContext) {
	if c != nil {
		c.Set(juiceContextKey, context)
	}
}

func GetJuiceContext(c *gin.Context) JuiceContext {
	if c == nil {
		return JuiceContext{}
	}
	value, ok := c.Get(juiceContextKey)
	if !ok {
		return JuiceContext{}
	}
	context, ok := value.(JuiceContext)
	if !ok {
		return JuiceContext{}
	}
	return context
}

func ResolveJuiceValue(context JuiceContext, model, reasoningEffort string) (int, bool) {
	if !context.Triggered {
		return 0, false
	}
	return juice_setting.Find(model, reasoningEffort)
}

func ReplaceJuiceNumber(text string, value int) (string, bool) {
	replacement := strconv.Itoa(value)
	if standaloneNumberPattern.MatchString(text) {
		return replacement, true
	}
	match := juiceNumberPattern.FindStringSubmatchIndex(text)
	if match == nil {
		return text, false
	}
	start, end := match[4], match[5]
	return text[:start] + replacement + text[end:], true
}

func normalizeJuiceText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
