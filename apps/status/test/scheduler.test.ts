import { afterEach, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import scheduler from '../worker/scheduler'

afterEach(() => vi.unstubAllGlobals())

const dispatchUrl = 'https://api.github.com/repos/YourTongji/YourTJ-Hub/actions/workflows/collect-status.yml/dispatches'
const runsUrl = 'https://api.github.com/repos/YourTongji/YourTJ-Hub/actions/workflows/collect-status.yml/runs?status=waiting&per_page=20'

function stubFetchSequence(...responses: Response[]) {
  const fetcher = vi.fn()
  for (const response of responses) fetcher.mockResolvedValueOnce(response)
  vi.stubGlobal('fetch', fetcher)
  return fetcher
}

it.each([
  ['2,17,32,47 * * * *', 'public'],
  ['7 * * * *', 'devices'],
])('dispatches %s to the fixed main collector without GitHub schedule events', async (cron, kind) => {
  const fetcher = stubFetchSequence(
    new Response(JSON.stringify({ workflow_runs: [] }), { status: 200 }),
    new Response(null, { status: 204 }),
  )
  await scheduler.scheduled({ cron }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' })
  expect(fetcher).toHaveBeenCalledTimes(2)
  expect(fetcher.mock.calls[0]![0]).toBe(runsUrl)
  expect(fetcher.mock.calls[0]![1].method).toBe('GET')
  const [url, init] = fetcher.mock.calls[1]!
  expect(url).toBe(dispatchUrl)
  expect(init.method).toBe('POST')
  expect(init.redirect).toBe('manual')
  expect(init.signal).toBeInstanceOf(AbortSignal)
  expect(init.headers.Authorization).toBe('Bearer test-only-token')
  expect(JSON.parse(init.body)).toEqual({ ref: 'main', inputs: { kind } })
})

it('force-cancels main collect-status runs waiting longer than 20 minutes before dispatch', async () => {
  const old = new Date(Date.now() - 21 * 60 * 1000).toISOString()
  const fresh = new Date(Date.now() - 5 * 60 * 1000).toISOString()
  const fetcher = stubFetchSequence(
    new Response(JSON.stringify({
      workflow_runs: [
        { id: 11, head_branch: 'main', status: 'waiting', created_at: old },
        { id: 12, head_branch: 'main', status: 'waiting', created_at: fresh },
        { id: 13, head_branch: 'dev', status: 'waiting', created_at: old },
      ],
    }), { status: 200 }),
    new Response(null, { status: 202 }),
    new Response(null, { status: 204 }),
  )
  await scheduler.scheduled({ cron: '7 * * * *' }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' })
  expect(fetcher.mock.calls.map(call => [call[0], call[1].method])).toEqual([
    [runsUrl, 'GET'],
    ['https://api.github.com/repos/YourTongji/YourTJ-Hub/actions/runs/11/force-cancel', 'POST'],
    [dispatchUrl, 'POST'],
  ])
})

it('continues dispatch when stuck-run cleanup fails', async () => {
  const fetcher = vi.fn()
    .mockRejectedValueOnce(new Error('list failed'))
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
  vi.stubGlobal('fetch', fetcher)
  await scheduler.scheduled({ cron: '7 * * * *' }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' })
  expect(fetcher).toHaveBeenCalledTimes(2)
  expect(fetcher.mock.calls[1]![0]).toBe(dispatchUrl)
})

it('fails closed for unknown schedules or missing credentials', async () => {
  const fetcher = vi.fn()
  vi.stubGlobal('fetch', fetcher)
  for (const cron of ['* * * * *', 'toString', '__proto__']) {
    await expect(scheduler.scheduled({ cron }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' })).rejects.toThrow('Unknown')
  }
  await expect(scheduler.scheduled({ cron: '7 * * * *' }, {})).rejects.toThrow('missing')
  expect(fetcher).not.toHaveBeenCalled()
})

it.each([200, 301, 401, 403, 429, 500])('reports rejected dispatch %s without reading its body or treating it as success', async status => {
  const response = new Response('private upstream error', { status })
  const fetcher = stubFetchSequence(
    new Response(JSON.stringify({ workflow_runs: [] }), { status: 200 }),
    response,
  )
  await expect(scheduler.scheduled({ cron: '7 * * * *' }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' }))
    .rejects.toThrow(`Status dispatch rejected (${status})`)
  expect(fetcher).toHaveBeenCalledTimes(2)
  expect(response.bodyUsed).toBe(true)
})

it('sanitizes network errors and does not retry an ambiguously accepted dispatch', async () => {
  const fetcher = vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ workflow_runs: [] }), { status: 200 }))
    .mockRejectedValueOnce(new Error('private upstream error'))
  vi.stubGlobal('fetch', fetcher)
  await expect(scheduler.scheduled({ cron: '7 * * * *' }, { GITHUB_DISPATCH_TOKEN: 'test-only-token' }))
    .rejects.toThrow('Status dispatch request failed')
  expect(fetcher).toHaveBeenCalledTimes(2)
})

it('never triggers collection for an HTTP request', () => {
  const fetcher = vi.fn()
  vi.stubGlobal('fetch', fetcher)
  expect(scheduler.fetch().status).toBe(404)
  expect(fetcher).not.toHaveBeenCalled()
})

it('isolates production scheduling from preview, public routes and stored provider data', () => {
  const config = JSON.parse(readFileSync('wrangler.scheduler.jsonc', 'utf8'))
  expect(config.triggers.crons).toEqual([])
  expect(config.workers_dev).toBe(false)
  expect(config.preview_urls).toBe(false)
  const production = config.env.production
  expect(production.triggers.crons).toEqual(['2,17,32,47 * * * *', '7 * * * *'])
  expect(production.workers_dev).toBe(false)
  expect(production.preview_urls).toBe(false)
  for (const target of [config, production]) {
    for (const binding of ['routes', 'r2_buckets', 'kv_namespaces', 'services', 'vars']) expect(target[binding]).toBeUndefined()
  }
})
