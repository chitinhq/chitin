import crypto from "crypto";

/**
 * Dimensions that define an agent's runtime identity for routing fingerprinting.
 */
export interface AgentFingerprintDimensions {
  driver: string | null;
  model: string | null;
  role: string | null;
  stationPromptHash: string | null;
  skillsToolsHash: string | null;
  soulLens: string | null;
}

/**
 * Computes a canonical agent fingerprint: deterministic hash + unhashed payload.
 * - Same input always yields same hash (sort lists before hashing externally)
 * - All 6 dimensions must be present; missing = null (not omitted)
 * - Hash is SHA-256, hex, truncated to 12 chars
 * - Soul lens: read from CHITIN_ACTIVE_SOUL env if not provided, else 'none'
 */
export function computeFingerprint(dimensions: Partial<AgentFingerprintDimensions>): {
  hash: string;
  payload: AgentFingerprintDimensions;
} {
  const soulLens = dimensions.soulLens ?? process.env.CHITIN_ACTIVE_SOUL ?? "none";
  const payload: AgentFingerprintDimensions = {
    driver: dimensions.driver ?? null,
    model: dimensions.model ?? null,
    role: dimensions.role ?? null,
    stationPromptHash: dimensions.stationPromptHash ?? null,
    skillsToolsHash: dimensions.skillsToolsHash ?? null,
    soulLens,
  };
  // Canonical JSON: stable key order, explicit nulls
  const canonical = JSON.stringify(payload);
  const hash = crypto.createHash("sha256").update(canonical).digest("hex").slice(0, 12);
  return { hash, payload };
}
