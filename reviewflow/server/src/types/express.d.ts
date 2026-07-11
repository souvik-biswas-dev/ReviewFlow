// Identity attached by the auth middleware. Declared as a module augmentation
// so handlers can read `req.userId` without casting at every call site.
declare global {
  namespace Express {
    interface Request {
      userId?: string;
      githubUsername?: string;
    }
  }
}

export {};
