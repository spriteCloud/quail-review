import { useState } from 'react';

interface LoginFormProps {
  onSubmit?: (email: string, password: string) => void;
}

/**
 * Sign-in form used by the Login page. Demo component for reviewqa's
 * form-fill + submit + multi-step navigation generation (v0.4.x).
 */
export function LoginForm({ onSubmit }: LoginFormProps) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  return (
    <form
      data-testid="login-form"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit?.(email, password);
      }}
    >
      <label>
        Email
        <input
          type="email"
          name="email"
          data-testid="login-email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </label>
      <label>
        Password
        <input
          type="password"
          name="password"
          data-testid="login-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </label>
      <button type="submit" data-testid="login-submit">
        Sign in
      </button>
      <a href="/forgot" data-testid="login-forgot">
        Forgot password?
      </a>
    </form>
  );
}
