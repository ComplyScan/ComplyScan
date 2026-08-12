package cli

import "errors"

type setupPromptStep func(promptSession) error

// runSetupPromptSteps runs a reversible sequence. Each completed step writes
// directly into caller-owned state, so returning to an earlier step preserves
// every answer and presents it as that step's default value.
func runSetupPromptSteps(prompt promptSession, allowReturn bool, steps ...setupPromptStep) error {
	for current := 0; current < len(steps); {
		stepPrompt := prompt
		stepPrompt.backAvailable = true
		if stepPrompt.questions != nil {
			stepPrompt.questions.current = current
		}
		err := steps[current](stepPrompt)
		if errors.Is(err, errPromptBack) {
			if current == 0 {
				if allowReturn {
					return errPromptBack
				}
				continue
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

func promptRevisitableRequiredChoice[T ~string](prompt promptSession, completed bool, current T, label string, allowed ...T) (T, error) {
	if completed {
		return promptChoice(prompt, label, current, allowed...)
	}
	return promptRequiredChoice(prompt, label, allowed...)
}

func setupTextPromptStep(guidanceKey, label string, target *string) setupPromptStep {
	return func(prompt promptSession) error {
		if err := explainSetupQuestion(prompt, guidanceKey); err != nil {
			return err
		}
		answer, err := prompt.text(label, *target)
		if err == nil {
			*target = answer
		}
		return err
	}
}

func setupTextListPromptStep(guidanceKey, label string, target *[]string) setupPromptStep {
	return func(prompt promptSession) error {
		if err := explainSetupQuestion(prompt, guidanceKey); err != nil {
			return err
		}
		answer, err := prompt.textList(label, *target)
		if err == nil {
			*target = answer
		}
		return err
	}
}

func setupRequiredChoicePromptStep[T ~string](guidanceKey, label string, target *T, completed *bool, allowed ...T) setupPromptStep {
	return func(prompt promptSession) error {
		if err := explainSetupQuestion(prompt, guidanceKey); err != nil {
			return err
		}
		answer, err := promptRevisitableRequiredChoice(prompt, *completed, *target, label, allowed...)
		if err == nil {
			*target, *completed = answer, true
		}
		return err
	}
}

func setupRequiredChoicesPromptStep[T ~string](guidanceKey, label string, target *[]T, completed *bool, allowed ...T) setupPromptStep {
	return func(prompt promptSession) error {
		if err := explainSetupQuestion(prompt, guidanceKey); err != nil {
			return err
		}
		var answer []T
		var err error
		if *completed {
			answer, err = promptChoices(prompt, label, *target, allowed...)
		} else {
			answer, err = promptRequiredChoices(prompt, label, allowed...)
		}
		if err == nil {
			*target, *completed = answer, true
		}
		return err
	}
}
