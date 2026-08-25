import { Link } from "react-router";

export function NotFound() {
  return (
    <div className="mx-auto max-w-md space-y-3 py-16 text-center">
      <h1 className="font-heading text-xl font-semibold">Not found</h1>
      <p className="text-xs/relaxed text-muted-foreground">
        Nothing lives at this address. Report links look like{" "}
        <code className="text-primary">/p/&lt;id&gt;</code>.
      </p>
      <Link to="/" className="inline-block text-xs/relaxed text-primary hover:underline">
        Back to the start
      </Link>
    </div>
  );
}
