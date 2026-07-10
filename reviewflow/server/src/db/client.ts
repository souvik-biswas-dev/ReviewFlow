import { MongoClient, type Collection, type Db, type Document } from 'mongodb';
import {
  AIReviewsCollection,
  NotificationsCollection,
  ReviewsCollection,
  SnippetsCollection,
  UsersCollection,
  type AIReviewDoc,
  type NotificationDoc,
  type ReviewDoc,
  type SnippetDoc,
  type UserDoc,
} from './models.ts';

/**
 * Wraps the raw MongoClient together with the resolved database handle, so
 * callers (routes, resolvers) don't have to repeat the database name. The typed
 * accessors below are the only place collection names meet document types.
 */
export class DBClient {
  readonly mongo: MongoClient;
  readonly database: Db;

  constructor(mongo: MongoClient, database: Db) {
    this.mongo = mongo;
    this.database = database;
  }

  private coll<T extends Document>(name: string): Collection<T> {
    return this.database.collection<T>(name);
  }

  get users(): Collection<UserDoc> {
    return this.coll<UserDoc>(UsersCollection);
  }

  get snippets(): Collection<SnippetDoc> {
    return this.coll<SnippetDoc>(SnippetsCollection);
  }

  get reviews(): Collection<ReviewDoc> {
    return this.coll<ReviewDoc>(ReviewsCollection);
  }

  get notifications(): Collection<NotificationDoc> {
    return this.coll<NotificationDoc>(NotificationsCollection);
  }

  get aiReviews(): Collection<AIReviewDoc> {
    return this.coll<AIReviewDoc>(AIReviewsCollection);
  }

  /** Liveness check used by /health. Throws if the primary is unreachable. */
  async ping(timeoutMS = 2000): Promise<void> {
    await this.database.command({ ping: 1 }, { timeoutMS });
  }

  /**
   * Cleanly closes the underlying connections. Call during graceful shutdown,
   * after the HTTP server has stopped accepting requests.
   */
  async disconnect(): Promise<void> {
    await this.mongo.close();
  }
}

/**
 * Dials MongoDB and verifies connectivity with a ping. It fails fast: if the
 * database is unreachable within the timeout, it throws and the caller is
 * expected to abort startup rather than serve an API that can't read or write
 * anything.
 */
export async function connect(uri: string, dbName: string): Promise<DBClient> {
  // A single 10s budget covers the whole connect + ping handshake, including
  // the initial server selection against a cold MongoDB container.
  const client = new MongoClient(uri, {
    serverSelectionTimeoutMS: 10_000,
    connectTimeoutMS: 10_000,
  });

  await client.connect();

  // connect() can resolve before a server is actually usable, so an explicit
  // ping against the primary is what surfaces an unreachable DB.
  const database = client.db(dbName);
  await database.command({ ping: 1 });

  console.log(`db: connected to MongoDB, using database ${JSON.stringify(dbName)}`);
  return new DBClient(client, database);
}
