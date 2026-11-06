export function compile(
  source: string,
  rules: Record<string, unknown>[],
  capabilities: Record<string, unknown>,
  tags: Record<string, unknown>,
): null | [number, number, string];
