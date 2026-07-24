/**
 * The bell-icon REST endpoints used by the dashboard. Notification rows are
 * written by the addReview mutation; this module only reads/marks them.
 */
import { Router, type Request, type Response } from 'express';
import { ObjectId } from 'mongodb';
import { authMiddleware } from '../auth/middleware.ts';
import type { Config } from '../config.ts';
import type { DBClient } from '../db/client.ts';
import type { NotificationDoc } from '../db/models.ts';

const DEFAULT_LIMIT = 20;
const MAX_PAGE = 50;

/** Wires GET /notifications and POST /notifications/read, both behind auth. */
export function notificationsRouter(cfg: Config, db: DBClient): Router {
  const router = Router();
  router.use(authMiddleware(cfg.jwtSecret));

  router.get('/', list(db));
  router.post('/read', markAllRead(db));

  return router;
}

/**
 * GET /notifications?page=N — the newest 20 notifications for the caller plus
 * an unreadCount for the bell badge.
 */
function list(db: DBClient) {
  return async (req: Request, res: Response): Promise<void> => {
    const userId = callerObjectId(req);
    if (!userId) {
      res.status(401).json({ error: 'unauthenticated' });
      return;
    }

    const page = parsePage(req.query.page);

    const rows = await db.notifications
      .find({ userId })
      .sort({ createdAt: -1 })
      .skip(page * DEFAULT_LIMIT)
      .limit(DEFAULT_LIMIT)
      .toArray();

    // Batch-load the referenced snippet titles in one round-trip.
    const titles = await loadSnippetTitles(db, rows);

    const notifications = rows.map((n) => ({
      id: n._id.toHexString(),
      snippetId: n.snippetId.toHexString(),
      snippetTitle: titles.get(n.snippetId.toHexString()) ?? '',
      reviewId: n.reviewId.toHexString(),
      read: n.read,
      createdAt: n.createdAt,
    }));

    // Non-fatal for the bell: a failed count just renders as zero.
    const unreadCount = await db.notifications
      .countDocuments({ userId, read: false })
      .catch(() => 0);

    res.json({ notifications, unreadCount, page });
  };
}

/**
 * POST /notifications/read — called when the bell dropdown opens; flips every
 * unread notification for the caller to read.
 */
function markAllRead(db: DBClient) {
  return async (req: Request, res: Response): Promise<void> => {
    const userId = callerObjectId(req);
    if (!userId) {
      res.status(401).json({ error: 'unauthenticated' });
      return;
    }

    await db.notifications.updateMany({ userId, read: false }, { $set: { read: true } });
    res.json({ ok: true });
  };
}

// --- helpers ---

function callerObjectId(req: Request): ObjectId | null {
  if (!req.userId) return null;
  try {
    return new ObjectId(req.userId);
  } catch {
    return null;
  }
}

function parsePage(raw: unknown): number {
  if (typeof raw !== 'string' || raw === '') return 0;
  const page = Number.parseInt(raw, 10);
  if (Number.isNaN(page) || page < 0) return 0;
  return Math.min(page, MAX_PAGE);
}

/** Snippet titles for a page of notifications, keyed by snippet hex id. */
async function loadSnippetTitles(
  db: DBClient,
  rows: NotificationDoc[],
): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  if (rows.length === 0) return out;

  const ids = [...new Map(rows.map((r) => [r.snippetId.toHexString(), r.snippetId])).values()];

  const titleRows = await db.snippets
    .find({ _id: { $in: ids } })
    .project<{ _id: ObjectId; title: string }>({ _id: 1, title: 1 })
    .toArray();

  for (const row of titleRows) out.set(row._id.toHexString(), row.title);
  return out;
}
