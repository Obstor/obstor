// Account labels and secret checks

// auth.CreateCredentials bounds for a secret key
export const SECRET_MIN = 8;
export const SECRET_MAX = 40;

export function accountLabel(account: string): string {
  switch (account) {
    case "root":
      return "Root";
    case "iam":
      return "IAM user";
    case "temporary":
      return "Temporary credentials";
    case "service":
      return "Service account";
    default:
      return "";
  }
}

// Compare secrets as typed
export function secretFormError(current: string, next: string, confirm: string): string | null {
  if (current === "") return "Enter your current secret key.";
  if (next.length < SECRET_MIN || next.length > SECRET_MAX) {
    return `New secret keys are ${SECRET_MIN} to ${SECRET_MAX} characters.`;
  }
  if (next !== confirm) return "The new secret keys do not match.";
  return null;
}
