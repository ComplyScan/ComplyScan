import { approveHumanReviewDecision } from "./review/approval.js";
import { requireReviewer } from "./security/authorization.js";

export function registerReviewRoutes(app) {
  app.post("/review", requireReviewer, approveHumanReviewDecision);
}

registerReviewRoutes(app);
