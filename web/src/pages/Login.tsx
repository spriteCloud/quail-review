import { LoginForm } from '../components/LoginForm';

/**
 * Login page. Renders the sign-in form and a link to the dashboard
 * landing target so reviewqa can detect a multi-step nav chain.
 */
export default function Login() {
  return (
    <main data-testid="login-page" role="main">
      <h1>Sign in to reviewqa</h1>
      <LoginForm />
      <p>
        Need to see the dashboard directly?{' '}
        <a href="/dashboard" data-testid="login-dashboard-link">
          Go to dashboard
        </a>
      </p>
    </main>
  );
}
