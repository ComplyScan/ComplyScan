import { aiAssistantDisclosure } from "../chat/disclosure";
import { attachSyntheticContentMetadata } from "../synthetic/provenance";
import { sanitizePromptInjectionInput } from "../security/prompt_injection";

export function generateContent(request: { prompt: string }): object {
  const prompt = sanitizePromptInjectionInput(request.prompt);
  return attachSyntheticContentMetadata({
    prompt,
    disclosure: aiAssistantDisclosure(),
  });
}

app.post("/generate", generateContent);
