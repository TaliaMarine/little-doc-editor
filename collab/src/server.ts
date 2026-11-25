import { buildServer } from './app.js'
import { config } from './config.js'
import { closePersistence } from './persistence.js'

// Upper bound on graceful shutdown. destroy() waits for every document to
// unload, and a document whose final store keeps failing never unloads — the
// pod would otherwise hang until SIGKILL.
const SHUTDOWN_TIMEOUT_MS = 20_000

const server = buildServer()

async function main() {
  await server.listen()
  console.log(`collab listening on :${config.port} ${config.metricsPort ? `(metrics on :${config.metricsPort}/metrics)` : '(metrics at /metrics)'}`)
}

let shuttingDown = false
async function shutdown() {
  if (shuttingDown) return
  shuttingDown = true
  const timeout = setTimeout(() => {
    console.error(`shutdown did not finish within ${SHUTDOWN_TIMEOUT_MS}ms; exiting`)
    process.exit(1)
  }, SHUTDOWN_TIMEOUT_MS)
  try {
    await server.destroy()
    await closePersistence()
  } finally {
    clearTimeout(timeout)
  }
  process.exit(0)
}

process.on('SIGINT', shutdown)
process.on('SIGTERM', shutdown)
process.on('SIGQUIT', shutdown)

main().catch((err) => {
  console.error('fatal', err)
  process.exit(1)
})
