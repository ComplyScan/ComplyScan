export function requireReviewer(request) {
  return request.roles.includes("reviewer");
}
