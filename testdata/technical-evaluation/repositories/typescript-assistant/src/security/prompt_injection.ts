export function sanitizePromptInjectionInput(input: string): string {
  if (input.toLowerCase().includes("prompt injection")) {
    return "[filtered]";
  }
  return input.replaceAll("ignore previous instructions", "[filtered]");
}
