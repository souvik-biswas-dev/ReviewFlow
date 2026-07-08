// Copies the GraphQL SDL next to the compiled output, since tsc only emits JS
// and schema.ts reads the .graphql file at runtime.
import { copyFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';

const root = join(import.meta.dirname, '..');
const src = join(root, 'src/graphql/schema.graphql');
const dest = join(root, 'dist/graphql/schema.graphql');

mkdirSync(dirname(dest), { recursive: true });
copyFileSync(src, dest);
console.log(`build: copied ${src} -> ${dest}`);
