// Full bucket S3 access, global admin actions require separate assignment
export function fullBucketPolicy(bucket: string): string {
  const resources = [`arn:aws:s3:::${bucket}`, `arn:aws:s3:::${bucket}/*`];
  return JSON.stringify(
    {
      Version: "2012-10-17",
      Statement: [{ Effect: "Allow", Action: ["s3:*"], Resource: resources }],
    },
    null,
    2,
  );
}

// Widen S3 access only, mixed admin statements need explicit edits
export function addBucketToPolicy(body: string, bucket: string): string {
  const policy = JSON.parse(body);
  if (!policy || typeof policy !== "object" || Array.isArray(policy)) {
    throw new Error("Enter a valid IAM policy before adding a bucket.");
  }
  const statements = Array.isArray(policy.Statement) ? policy.Statement : [policy.Statement];
  let changed = false;
  for (const statement of statements) {
    if (statement?.Effect !== "Allow") continue;
    const actions = Array.isArray(statement.Action) ? statement.Action : [statement.Action];
    const s3 = actions.some(
      (action: unknown) => typeof action === "string" && action.startsWith("s3:"),
    );
    if (!s3) continue;
    if (
      !actions.every((action: unknown) => typeof action === "string" && action.startsWith("s3:"))
    ) {
      throw new Error("Statements that mix S3 and administrator actions must be edited directly.");
    }
    const resources = Array.isArray(statement.Resource)
      ? statement.Resource
      : statement.Resource
        ? [statement.Resource]
        : [];
    statement.Resource = [
      ...new Set([...resources, `arn:aws:s3:::${bucket}`, `arn:aws:s3:::${bucket}/*`]),
    ];
    changed = true;
  }
  if (!changed) throw new Error("This policy has no S3 Allow statement. Edit the JSON directly.");
  policy.Statement = statements;
  return JSON.stringify(policy, null, 2);
}
