import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { makeExecutableSchema } from '@graphql-tools/schema';
import { GraphQLError, GraphQLScalarType, Kind, type GraphQLSchema } from 'graphql';
import { resolvers } from './resolvers.ts';

/**
 * RFC-3339 timestamps. Internally every date is a JS `Date`; on the wire it is
 * an ISO-8601 string, which is what the SvelteKit client parses.
 */
const TimeScalar = new GraphQLScalarType<Date, string>({
  name: 'Time',
  description: 'An RFC-3339 / ISO-8601 timestamp.',

  serialize(value): string {
    if (value instanceof Date) return value.toISOString();
    if (typeof value === 'string') return new Date(value).toISOString();
    if (typeof value === 'number') return new Date(value).toISOString();
    throw new GraphQLError(`Time cannot represent value: ${String(value)}`);
  },

  parseValue(value): Date {
    if (typeof value !== 'string') {
      throw new GraphQLError('Time must be an ISO-8601 string');
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) throw new GraphQLError(`Invalid Time: ${value}`);
    return date;
  },

  parseLiteral(ast): Date {
    if (ast.kind !== Kind.STRING) {
      throw new GraphQLError('Time must be an ISO-8601 string', { nodes: ast });
    }
    const date = new Date(ast.value);
    if (Number.isNaN(date.getTime())) throw new GraphQLError(`Invalid Time: ${ast.value}`);
    return date;
  },
});

/**
 * Builds the executable schema from the SDL file. The .graphql file is the
 * single source of truth for the API surface and is copied next to the compiled
 * output by the build step (see scripts/copy-schema.mjs).
 */
export function buildSchema(): GraphQLSchema {
  const typeDefs = readFileSync(join(import.meta.dirname, 'schema.graphql'), 'utf8');
  return makeExecutableSchema({
    typeDefs,
    resolvers: { Time: TimeScalar, ...resolvers },
  });
}
