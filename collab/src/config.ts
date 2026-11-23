function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} is required`)
  }
  return value
}

export const config = {
  port: Number(process.env.PORT ?? 1234),
  databaseUrl: required('DATABASE_URL'),
  redisAddr: required('REDIS_ADDR'),
  sessionJwtSecret: required('SESSION_JWT_SECRET'),
  // Optional Redis AUTH password (Helm: redis.auth); unset for a Redis
  // without auth, e.g. the local docker-compose one.
  redisPassword: process.env.REDIS_PASSWORD || undefined,
}
