package context

import (
	"fmt"
	"strings"
)

// getCompactPrompt returns the compact prompt that instructs the LLM to summarize
// the conversation. Follows the CC 9-section summarization structure (Architecture Decision 33).
func getCompactPrompt(customInstructions string) string {
	var sb strings.Builder

	sb.WriteString(`You are tasked with summarizing the above conversation. Your summary will be used to continue this conversation after compaction. The summary must capture ALL critical information needed to continue the work seamlessly.

DO NOT use any tools. Only produce a text summary.

First, analyze the conversation in <analysis> tags (this will be stripped), then produce your summary in <summary> tags.

In <analysis>, draft your thinking about what's important. In <summary>, produce the final output.

Your summary MUST include these sections (skip any that don't apply):

1. **Primary Request and Intent**
   The user's main request/goal. What are they trying to accomplish? Include the original wording if possible.

2. **Key Technical Concepts**
   Important technical details, constraints, patterns, or domain knowledge established during the conversation.

3. **Files and Code Sections**
   All files read, created, or modified. Include file paths, key functions/types, and important code snippets or structural decisions.

4. **Errors and Fixes**
   Any errors encountered and how they were resolved. Include error messages and fix strategies.

5. **Problem Solving**
   Important problem-solving approaches tried, what worked and what didn't.

6. **All User Messages**
   Preserve ALL distinct user messages that are NOT tool results. These are critical for understanding intent and preferences.

7. **Pending Tasks**
   Any incomplete tasks, TODOs, or next steps that were mentioned but not yet completed.

8. **Current Work**
   What was being actively worked on at the point of compaction. Include the current state/progress.

9. **Optional Next Step**
   If there's a clear next action to take, state it explicitly.

Format the summary as a continuation message:
"This session is being continued from a previous conversation that ran out of context. Here is a summary of the key points:"

Then present the sections clearly. Be thorough — losing context is worse than being verbose.`)

	if customInstructions != "" {
		fmt.Fprintf(&sb, "\n\nAdditional instructions:\n%s", customInstructions)
	}

	return sb.String()
}
