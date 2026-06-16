/**
 * Dashboard page — target of the post-login redirect / multi-step nav.
 * Minimal markup with stable test ids so reviewqa can assert the
 * navigation landed correctly.
 */
export default function Dashboard() {
  return (
    <main data-testid="dashboard-page" role="main" aria-label="Customer dashboard">
      <h1 data-testid="dashboard-heading">Welcome back</h1>
      <section data-testid="dashboard-summary">
        <h2>Account summary</h2>
        <p>Latest activity will appear here.</p>
      </section>
      <a href="/" data-testid="dashboard-home-link">
        Back to home
      </a>
    </main>
  );
}
