export function attachSyntheticContentMetadata(content: object): object {
  return {
    ...content,
    syntheticContent: true,
    provenanceMetadata: "AI generated content",
  };
}
