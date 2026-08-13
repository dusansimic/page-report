import { Link } from "react-router";

export function NotFound() {
  return (
    <div className="mx-auto max-w-md space-y-3 py-16 text-center">
      <h1 className="text-2xl font-semibold">Not found</h1>
      <p className="text-muted">
        Nothing lives at this address. Report links look like{" "}
        <code className="text-accent">/p/&lt;id&gt;</code>.
      </p>
      <Link to="/" className="inline-block text-accent hover:underline">
        Back to the start
      </Link>
    </div>
  );
}
