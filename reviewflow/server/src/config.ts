/**
 * Runtime configuration, loaded from environment variables.
 *
 * We deliberately avoid a third-party env library (dotenv, envalid, zod-env): a
 * tiny helper around process.env keeps the dependency surface small and the
 * behavior obvious. In Docker Compose the values arrive via `env_file: .env`;
 * when running `npm run dev` locally, Node's own `--env-file-if-exists` flag
 * (see package.json) loads them.
 */
export interface Config {
  port: number; // HTTP port the server listens on
  mongoURI: string; // MongoDB connection string
  mongoDB: string; // Database name to use
  environment: string; // "development" | "production"
  corsOrigin: string; // Allowed browser origin (the SvelteKit dev server)

  // --- Auth / sessions ---
  jwtSecret: string; // HMAC secret used to sign session JWTs

  // --- GitHub OAuth (register at https://github.com/settings/developers) ---
  githubClientID: string;
  githubClientSecret: string;
  githubRedirectURL: string; // must exactly match the callback URL registered on GitHub
  frontendURL: string; // where the browser is sent after a successful login

  // --- AI (Google Gemini, free tier) ---
  geminiAPIKey: string; // if empty, AI reviews are disabled (app still runs)
}

const INSECURE_DEV_SECRET = 'dev-insecure-secret-change-me';

/**
 * Reads configuration from the process environment, applying sane defaults for
 * local development so the app boots with minimal config.
 */
export function loadConfig(): Config {
  const cfg: Config = {
    port: Number(getEnv('PORT', '8080')),
    mongoURI: getEnv('MONGO_URI', 'mongodb://localhost:27017'),
    mongoDB: getEnv('MONGO_DB', 'reviewflow'),
    environment: getEnv('ENVIRONMENT', 'development'),
    corsOrigin: getEnv('CORS_ORIGIN', 'http://localhost:5173'),

    jwtSecret: getEnv('JWT_SECRET', INSECURE_DEV_SECRET),
    githubClientID: getEnv('GITHUB_CLIENT_ID', ''),
    githubClientSecret: getEnv('GITHUB_CLIENT_SECRET', ''),
    githubRedirectURL: getEnv('GITHUB_REDIRECT_URL', 'http://localhost:8080/auth/github/callback'),
    frontendURL: getEnv('FRONTEND_URL', 'http://localhost:5173'),

    geminiAPIKey: getEnv('GEMINI_API_KEY', ''),
  };

  if (!Number.isInteger(cfg.port) || cfg.port <= 0 || cfg.port > 65535) {
    console.error(`config: PORT must be a valid port number, got ${process.env.PORT}`);
    process.exit(1);
  }

  // Fail/warn loudly for anything that makes auth insecure or non-functional,
  // so problems surface at startup rather than as confusing 401s later.
  if (cfg.environment === 'production' && cfg.jwtSecret === INSECURE_DEV_SECRET) {
    console.error('config: JWT_SECRET must be set to a strong value in production');
    process.exit(1);
  }
  if (!cfg.githubClientID || !cfg.githubClientSecret) {
    console.log('config: GITHUB_CLIENT_ID/SECRET not set — /auth/github will not work until configured');
  }
  if (!cfg.geminiAPIKey) {
    console.log('config: GEMINI_API_KEY not set — AI code reviews are disabled');
  }

  return cfg;
}

/** True when we should apply production-grade defaults (Secure cookies, etc). */
export function isProduction(cfg: Config): boolean {
  return cfg.environment === 'production';
}

/**
 * Returns the value of the named env var, or fallback if it is unset or empty.
 * We log when a default is applied so misconfiguration is visible early.
 */
function getEnv(key: string, fallback: string): string {
  const v = process.env[key];
  if (v !== undefined && v !== '') return v;
  console.log(`config: ${key} not set, using default ${JSON.stringify(fallback)}`);
  return fallback;
}
