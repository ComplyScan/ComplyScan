import { persistResult } from "./store";

export async function deadOverrideDecision(): Promise<void> {
  await persistResult();
}
