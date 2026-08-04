export function approveHumanReviewDecision(request) {
  return {
    status: "approved",
    decision: request.pendingDecision,
    review: "human review approval",
  };
}
