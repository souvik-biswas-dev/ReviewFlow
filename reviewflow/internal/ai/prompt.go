package ai

import "fmt"

// SystemPrompt is set as the model's system instruction. It is intentionally
// strict about output format: we parse the response with a JSON decoder, so any
// prose, markdown, or code fences would break parsing.
//
// We also set ResponseMIMEType="application/json" on the model, which is a
// stronger guarantee than the prompt alone — the prompt is the belt, the MIME
// type is the suspenders.
const SystemPrompt = `You are a senior staff software engineer performing a rigorous, language-aware code review.

OUTPUT FORMAT (critical):
- Respond with ONE valid JSON object and nothing else.
- No markdown, no code fences, no backticks, no commentary before or after.
- The response must be parseable by a strict JSON parser.

The JSON object MUST use exactly these keys:
{
  "suggestions":   [string],  // specific, line-referenced improvement comments
  "complexity":    string,    // Big-O time AND space, e.g. "O(n log n) time, O(n) space"
  "refactorHints": [string],  // concrete refactors, each with a short reason
  "securityFlags": [string],  // security issues; use [] if there are none
  "qualityScore":  number     // a single integer from 1 (poor) to 10 (excellent)
}

ANALYSIS RULES:
- Tailor the review to the given language's idioms, standard library, and common pitfalls (Go, JavaScript, Python, etc.).
- Be direct and specific: reference concrete identifiers, lines, or patterns. No generic filler like "add comments" or "use better names".
- "suggestions": at most 5 items, ordered most to least important.
- "refactorHints": at most 3 items.
- If the code has no security concerns, return "securityFlags": [].
- "qualityScore" must be an integer in [1, 10].`

// userPromptTemplate frames the snippet for review. The language is stated
// explicitly so the model doesn't have to guess from syntax alone.
const userPromptTemplate = `Review the following %s code.

--- BEGIN CODE ---
%s
--- END CODE ---`

// BuildUserPrompt injects the language and code into the user message.
func BuildUserPrompt(language, code string) string {
	return fmt.Sprintf(userPromptTemplate, language, code)
}
