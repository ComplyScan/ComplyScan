// complyscan:ignore-ai-signals -- this file defines detection signatures.
package inventory

type componentDefinition struct {
	Name      string
	Kind      ComponentKind
	Packages  map[string][]string
	Endpoints []string
	EnvVars   []string
}

var componentDefinitions = []componentDefinition{
	{
		Name: "OpenAI", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"openai"},
			"node":   {"openai"},
			"go":     {"github.com/openai/openai-go", "github.com/sashabaranov/go-openai"},
			"java":   {"com.openai"},
		},
		Endpoints: []string{"https://api.openai.com", "http://api.openai.com"},
		EnvVars:   []string{"OPENAI_API_KEY"},
	},
	{
		Name: "Anthropic", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"anthropic"},
			"node":   {"@anthropic-ai/sdk"},
			"go":     {"github.com/anthropics/anthropic-sdk-go"},
			"java":   {"com.anthropic"},
		},
		Endpoints: []string{"https://api.anthropic.com", "http://api.anthropic.com"},
		EnvVars:   []string{"ANTHROPIC_API_KEY"},
	},
	{
		Name: "Google Gemini", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"google.generativeai", "google.genai", "google-generativeai", "google-genai"},
			"node":   {"@google/generative-ai", "@google/genai"},
			"go":     {"google.golang.org/genai"},
			"java":   {"com.google.genai", "com.google.cloud.vertexai"},
		},
		Endpoints: []string{"https://generativelanguage.googleapis.com", "http://generativelanguage.googleapis.com"},
		EnvVars:   []string{"GEMINI_API_KEY"},
	},
	{
		Name: "Mistral", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"mistralai"},
			"node":   {"@mistralai/mistralai"},
			"go":     {"github.com/gage-technologies/mistral-go"},
			"java":   {"ai.mistral"},
		},
		Endpoints: []string{"https://api.mistral.ai", "http://api.mistral.ai"},
		EnvVars:   []string{"MISTRAL_API_KEY"},
	},
	{
		Name: "Cohere", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"cohere"},
			"node":   {"cohere-ai"},
			"go":     {"github.com/cohere-ai/cohere-go"},
			"java":   {"com.cohere"},
		},
		Endpoints: []string{"https://api.cohere.com", "http://api.cohere.com"},
		EnvVars:   []string{"COHERE_API_KEY"},
	},
	{
		Name: "Hugging Face", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"huggingface_hub", "huggingface-hub", "transformers"},
			"node":   {"@huggingface/inference", "@huggingface/transformers"},
		},
		Endpoints: []string{"https://api-inference.huggingface.co", "http://api-inference.huggingface.co"},
		EnvVars:   []string{"HF_TOKEN", "HUGGINGFACE_TOKEN"},
	},
	{
		Name: "Ollama", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"ollama"},
			"node":   {"ollama"},
			"go":     {"github.com/ollama/ollama/api"},
		},
		Endpoints: []string{"http://localhost:11434", "https://localhost:11434", "http://127.0.0.1:11434"},
		EnvVars:   []string{"OLLAMA_HOST"},
	},
	{
		Name: "LiteLLM", Kind: KindFramework,
		Packages: map[string][]string{
			"python": {"litellm"},
			"node":   {"litellm"},
		},
		EnvVars: []string{"LITELLM_API_KEY"},
	},
	{
		Name: "LangChain", Kind: KindFramework,
		Packages: map[string][]string{
			"python": {"langchain", "langchain-openai", "langchain-anthropic", "langchain_google_genai"},
			"node":   {"langchain", "@langchain/core", "@langchain/openai", "@langchain/anthropic", "@langchain/google-genai"},
			"go":     {"github.com/tmc/langchaingo"},
		},
	},
	{
		Name: "LlamaIndex", Kind: KindFramework,
		Packages: map[string][]string{
			"python": {"llama_index", "llama-index"},
			"node":   {"llamaindex"},
		},
	},
	{
		Name: "Vercel AI SDK", Kind: KindFramework,
		Packages: map[string][]string{
			"node": {"ai", "@ai-sdk/openai", "@ai-sdk/anthropic", "@ai-sdk/google", "@ai-sdk/mistral", "@ai-sdk/cohere"},
		},
	},
	{
		Name: "OpenRouter", Kind: KindProvider,
		Packages: map[string][]string{
			"python": {"openrouter"},
			"node":   {"@openrouter/ai-sdk-provider"},
		},
		Endpoints: []string{"https://openrouter.ai/api", "http://openrouter.ai/api"},
		EnvVars:   []string{"OPENROUTER_API_KEY"},
	},
}
