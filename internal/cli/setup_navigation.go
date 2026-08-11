package cli

import "errors"

type setupPromptStep func(promptSession) error

// runSetupPromptSteps runs a reversible sequence. Each completed step writes
// directly into caller-owned state, so returning to an earlier step preserves
// every answer and presents it as that step's default value.
func runSetupPromptSteps(prompt promptSession, allowReturn bool, steps ...setupPromptStep) error {
	for current := 0; current < len(steps); {
		stepPrompt := prompt
		stepPrompt.backAvailable = current > 0 || allowReturn
		if stepPrompt.questions != nil {
			stepPrompt.questions.current = current
		}
		err := steps[current](stepPrompt)
		if errors.Is(err, errPromptBack) {
			if current == 0 {
				return errPromptBack
			}
			current--
			continue
		}
		if err != nil {
			return err
		}
		current++
	}
	return nil
}
