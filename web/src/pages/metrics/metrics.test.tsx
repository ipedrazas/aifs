import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { loggedIn, mockApi } from '../../test/fetch'
import { formatPercent, formatSeconds } from '../../lib/format'

const version = { status: 200, body: { version: 'v1', commit: 'c', schemaVersion: 8 } }

function metrics(over: Record<string, unknown> = {}) {
  return {
    generations: 2,
    variations: 5,
    images: 5,
    pending: 1,
    reviewed: 4,
    approved: 2,
    needsRefinement: 0,
    rejected: { fidelity: 1, photo_quality: 1 },
    approvalRate: 0.5,
    rejectionRates: { fidelity: 0.25, photo_quality: 0.25 },
    fidelityPassRate: 0.75,
    costMicros: 70_000,
    costPerApprovedMicros: 35_000,
    avgGenerationSeconds: 12,
    avgTimeToApprovalSeconds: 3700,
    garments: 1,
    usableImagesPerGarment: 2,
    ...over,
  }
}

const providers = {
  status: 200,
  body: {
    maxVariations: 4,
    providers: [{ name: 'fake', label: 'Fake', available: true, reason: '', capabilities: [], models: {}, unitPriceMicros: 0, recipes: [] }],
    recipes: [{ name: 'reference', label: 'Reference-guided', description: '', steps: [] }],
  },
}

function visit(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

describe('metrics', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows the headline figures and a row per group, and filters through the URL', async () => {
    const queries: string[] = []
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/providers': providers,
      'GET /api/v1/products': { status: 200, body: { items: [] } },
      'GET /api/v1/metrics/summary': (req) => {
        queries.push(new URL(req.url).search)
        return {
          status: 200,
          body: {
            groupBy: 'pipeline',
            total: metrics(),
            groups: [
              { key: 'reference|fake', label: 'reference · fake', ...metrics({ approved: 1, approvalRate: 1 / 3 }) },
              { key: 'tryon|fake', label: 'tryon · fake', ...metrics({ approved: 1 }) },
            ],
          },
        }
      },
    })
    const user = userEvent.setup()
    visit('/metrics?testSet=true')

    const summary = await screen.findByLabelText('Summary')
    expect(within(summary).getByText('75%')).toBeInTheDocument()
    expect(within(summary).getByText('$0.04')).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'Metrics by group' })
    expect(within(table).getByRole('rowheader', { name: 'reference · fake' })).toBeInTheDocument()
    expect(within(table).getByText('33.3%')).toBeInTheDocument()
    expect(within(table).getByRole('rowheader', { name: 'Total' })).toBeInTheDocument()
    expect(screen.getByLabelText('Test set only')).toBeChecked()
    expect(queries[0]).toContain('testSet=true')

    await user.selectOptions(screen.getByLabelText('Group by'), 'printComplexity')
    await waitFor(() => expect(queries.at(-1)).toContain('groupBy=printComplexity'))
    expect(window.location.search).toContain('groupBy=printComplexity')
  })

  it('says when nothing matches', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/providers': providers,
      'GET /api/v1/products': { status: 200, body: { items: [] } },
      'GET /api/v1/metrics/summary': {
        status: 200,
        body: { groupBy: 'pipeline', total: metrics({ generations: 0, reviewed: 0, approvalRate: null }), groups: [] },
      },
    })
    visit('/metrics')
    expect(await screen.findByText('No generations match these filters.')).toBeInTheDocument()
  })

  it('formats rates and durations', () => {
    expect(formatPercent(null)).toBe('–')
    expect(formatPercent(0.8)).toBe('80%')
    expect(formatSeconds(65)).toBe('1m 05s')
    expect(formatSeconds(3700)).toBe('1h 01m')
    expect(formatSeconds(3 * 86_400)).toBe('3d')
  })
})
