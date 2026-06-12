/**
 * In-memory rate limiter for the dump API.
 * Max 1 dump per 5 minutes per service ID.
 * Memory is cleared on server restart — acceptable for this use case.
 */

const cooldown = new Map<string, number>();

/** Check if a service is allowed to dump, or return the wait time in ms. */
export function checkRateLimit(serviceId: string, intervalMs = 5 * 60 * 1000): number | null {
  const last = cooldown.get(serviceId);
  const now = Date.now();
  if (last && now - last < intervalMs) {
    return intervalMs - (now - last); // remaining wait ms
  }
  cooldown.set(serviceId, now);
  return null; // allowed
}
