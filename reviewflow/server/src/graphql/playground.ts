/**
 * The interactive query explorer served at GET /graphql/playground, replacing
 * gqlgen's built-in playground handler.
 *
 * Like that handler, this is a thin HTML shell that pulls GraphiQL from a CDN —
 * no assets are bundled into the server image, and the page is only useful to a
 * developer with a browser open.
 */
export function playgroundHTML(endpoint: string): string {
  return `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>ReviewFlow — GraphQL</title>
    <link rel="stylesheet" href="https://unpkg.com/graphiql@3/graphiql.min.css" />
    <style>
      body { margin: 0; height: 100dvh; }
      #graphiql { height: 100dvh; }
    </style>
  </head>
  <body>
    <div id="graphiql">Loading GraphiQL…</div>

    <script src="https://unpkg.com/react@18/umd/react.production.min.js"></script>
    <script src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js"></script>
    <script src="https://unpkg.com/graphiql@3/graphiql.min.js"></script>
    <script>
      const fetcher = GraphiQL.createFetcher({
        url: ${JSON.stringify(endpoint)},
        // Send the session cookie so authenticated queries (me, mutations)
        // work straight from the explorer.
        fetch: (url, opts) => fetch(url, { ...opts, credentials: 'include' }),
      });
      ReactDOM.createRoot(document.getElementById('graphiql')).render(
        React.createElement(GraphiQL, { fetcher, defaultEditorToolsVisibility: true })
      );
    </script>
  </body>
</html>`;
}
