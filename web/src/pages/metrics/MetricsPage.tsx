import { useSearchParams } from 'react-router'
import type { Metrics, MetricsGrouping } from '../../api/client'
import { useProducts } from '../../api/products'
import { useMetrics, useProviders, type MetricsQuery } from '../../api/studio'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { formatCost, formatPercent, formatSeconds } from '../../lib/format'

const GROUPINGS: { value: MetricsGrouping; label: string }[] = [
  { value: 'pipeline', label: 'Recipe and providers' },
  { value: 'recipe', label: 'Recipe' },
  { value: 'provider', label: 'Providers' },
  { value: 'product', label: 'Product' },
  { value: 'printComplexity', label: 'Print complexity' },
  { value: 'garmentRefs', label: 'Garment photos sent' },
  { value: 'preset', label: 'Preset' },
]

const fieldClass = 'w-full rounded border border-stone-300 px-2 py-1.5 text-sm focus:border-stone-500 focus:outline-none'

/** Reads the filters from the URL so a view can be shared and reloaded. */
function useFilters(): [MetricsQuery, (patch: Partial<Record<keyof MetricsQuery, string>>) => void] {
  const [params, setParams] = useSearchParams()
  const q: MetricsQuery = {
    groupBy: (params.get('groupBy') as MetricsGrouping) || 'pipeline',
  }
  for (const k of ['provider', 'recipe', 'from', 'to'] as const) {
    const v = params.get(k)
    if (v) q[k] = v
  }
  if (params.get('productId')) q.productId = Number(params.get('productId'))
  if (params.get('testSet') === 'true') q.testSet = true
  const set = (patch: Partial<Record<keyof MetricsQuery, string>>) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        for (const [k, v] of Object.entries(patch)) {
          if (v) next.set(k, v)
          else next.delete(k)
        }
        return next
      },
      { replace: true },
    )
  return [q, set]
}

export function MetricsPage() {
  const [q, set] = useFilters()
  const metrics = useMetrics(q)
  const providers = useProviders()
  const products = useProducts()
  const sum = metrics.data

  return (
    <section className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Metrics</h1>
        <p className="mt-1 text-sm text-stone-600">
          Rates are over reviewed images; pending images are counted but not rated. Cost includes failed and rejected work.
        </p>
      </div>

      <form className="grid gap-3 rounded border border-stone-200 bg-white p-4 sm:grid-cols-3 lg:grid-cols-7" aria-label="Filters">
        <Field id="m-group" label="Group by">
          <select id="m-group" value={q.groupBy} onChange={(e) => set({ groupBy: e.target.value })} className={fieldClass}>
            {GROUPINGS.map((g) => (
              <option key={g.value} value={g.value}>
                {g.label}
              </option>
            ))}
          </select>
        </Field>
        <Field id="m-provider" label="Provider">
          <select id="m-provider" value={q.provider ?? ''} onChange={(e) => set({ provider: e.target.value })} className={fieldClass}>
            <option value="">All</option>
            {providers.data?.providers.map((p) => (
              <option key={p.name} value={p.name}>
                {p.label}
              </option>
            ))}
          </select>
        </Field>
        <Field id="m-recipe" label="Recipe">
          <select id="m-recipe" value={q.recipe ?? ''} onChange={(e) => set({ recipe: e.target.value })} className={fieldClass}>
            <option value="">All</option>
            {providers.data?.recipes.map((r) => (
              <option key={r.name} value={r.name}>
                {r.label}
              </option>
            ))}
          </select>
        </Field>
        <Field id="m-product" label="Product">
          <select
            id="m-product"
            value={q.productId ?? ''}
            onChange={(e) => set({ productId: e.target.value })}
            className={fieldClass}
          >
            <option value="">All</option>
            {products.data?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </Field>
        <Field id="m-from" label="From">
          <input id="m-from" type="date" value={q.from ?? ''} onChange={(e) => set({ from: e.target.value })} className={fieldClass} />
        </Field>
        <Field id="m-to" label="To">
          <input id="m-to" type="date" value={q.to ?? ''} onChange={(e) => set({ to: e.target.value })} className={fieldClass} />
        </Field>
        <label htmlFor="m-testset" className="flex items-end gap-2 pb-2 text-sm text-stone-700">
          <input
            id="m-testset"
            type="checkbox"
            checked={q.testSet ?? false}
            onChange={(e) => set({ testSet: e.target.checked ? 'true' : '' })}
          />
          Test set only
        </label>
      </form>

      <ErrorMessage error={metrics.error} />
      {metrics.isPending && <p className="text-stone-500">Loading…</p>}
      {sum && sum.total.generations === 0 && <p className="text-stone-600">No generations match these filters.</p>}
      {sum && sum.total.generations > 0 && (
        <>
          <Headline m={sum.total} />
          <div className="overflow-x-auto rounded border border-stone-200 bg-white">
            <table className="w-full text-sm" aria-label="Metrics by group">
              <thead className="bg-stone-50 text-left text-xs text-stone-600">
                <tr>
                  {[
                    GROUPINGS.find((g) => g.value === sum.groupBy)?.label ?? 'Group',
                    'Images',
                    'Reviewed',
                    'Approved',
                    'Fidelity pass',
                    'Rejected: fidelity',
                    'Rejected: photo quality',
                    'Rejected: other',
                    'Needs refinement',
                    'Cost / approved',
                    'Time to image',
                    'Time to approval',
                    'Usable / garment',
                  ].map((h, i) => (
                    <th key={h} scope="col" className={`px-3 py-2 font-medium ${i > 0 ? 'text-right' : ''}`}>
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sum.groups.map((g) => (
                  <Row key={g.key} label={g.label} m={g} />
                ))}
                <Row label="Total" m={sum.total} total />
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  )
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="block text-xs text-stone-600">
        {label}
      </label>
      {children}
    </div>
  )
}

/** The most important figures for the whole selection. */
function Headline({ m }: { m: Metrics }) {
  const tiles = [
    { label: 'Fidelity pass rate', value: formatPercent(m.fidelityPassRate), hint: 'reviewed images not rejected for fidelity' },
    { label: 'Approval rate', value: formatPercent(m.approvalRate), hint: `${m.approved} of ${m.reviewed} reviewed` },
    { label: 'Cost per approved image', value: m.costPerApprovedMicros == null ? '–' : formatCost(m.costPerApprovedMicros), hint: `${formatCost(m.costMicros)} spent` },
    { label: 'Usable images per garment', value: m.usableImagesPerGarment == null ? '–' : m.usableImagesPerGarment.toFixed(1), hint: `${m.garments} garment${m.garments === 1 ? '' : 's'}` },
    { label: 'Awaiting review', value: String(m.pending), hint: `${m.images} of ${m.variations} requested images produced` },
  ]
  return (
    <dl className="grid grid-cols-2 gap-3 md:grid-cols-5" aria-label="Summary">
      {tiles.map((t) => (
        <div key={t.label} className="rounded border border-stone-200 bg-white p-3">
          <dt className="text-xs text-stone-500">{t.label}</dt>
          <dd className="text-2xl font-semibold tabular-nums">{t.value}</dd>
          <dd className="text-xs text-stone-500">{t.hint}</dd>
        </div>
      ))}
    </dl>
  )
}

function Row({ label, m, total }: { label: string; m: Metrics; total?: boolean }) {
  const cells = [
    `${m.images}/${m.variations}`,
    `${m.reviewed}${m.pending ? ` (+${m.pending} pending)` : ''}`,
    formatPercent(m.approvalRate),
    formatPercent(m.fidelityPassRate),
    formatPercent(m.reviewed ? m.rejectionRates.fidelity ?? 0 : null),
    formatPercent(m.reviewed ? m.rejectionRates.photo_quality ?? 0 : null),
    formatPercent(m.reviewed ? m.rejectionRates.other ?? 0 : null),
    String(m.needsRefinement),
    m.costPerApprovedMicros == null ? '–' : formatCost(m.costPerApprovedMicros),
    formatSeconds(m.avgGenerationSeconds),
    formatSeconds(m.avgTimeToApprovalSeconds),
    m.usableImagesPerGarment == null ? '–' : m.usableImagesPerGarment.toFixed(1),
  ]
  return (
    <tr className={`border-t border-stone-100 ${total ? 'bg-stone-50 font-medium' : ''}`}>
      <th scope="row" className="px-3 py-2 text-left font-medium">
        {label}
      </th>
      {cells.map((c, i) => (
        <td key={i} className="px-3 py-2 text-right tabular-nums">
          {c}
        </td>
      ))}
    </tr>
  )
}
