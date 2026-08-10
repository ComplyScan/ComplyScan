import Fastify from "fastify";
import OpenAI from "openai";

const app = Fastify();
const client = new OpenAI();

app.post("/generate-image", async (request) => {
  const prompt = (request.body as { prompt: string }).prompt;
  const image = await client.images.generate({ model: "image-model", prompt });
  return {
    image: image.data[0],
    provenance: { aiGenerated: true, generator: "image-model" },
  };
});

app.listen({ host: "0.0.0.0", port: 8080 });
