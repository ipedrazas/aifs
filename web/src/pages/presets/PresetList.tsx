import { usePresets } from '../../api/studio'
import { ErrorMessage } from '../../components/ui/ErrorMessage'

const FIELDS = [
  ['environment', 'Environment'],
  ['pose', 'Pose'],
  ['framing', 'Framing'],
  ['lighting', 'Lighting'],
  ['style', 'Style'],
  ['promptFragment', 'Notes'],
] as const

export function PresetList() {
  const { data, error, isPending } = usePresets()

  return (
    <section className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Presets</h1>
        <p className="mt-1 text-sm text-stone-600">
          A preset sets the scene of a photoshoot. It never describes the garment: the reference photographs do.
        </p>
      </div>
      {isPending && <p className="text-stone-500">Loading…</p>}
      <ErrorMessage error={error} />
      <ul className="grid gap-4 md:grid-cols-2">
        {data?.map((p) => (
          <li key={p.id} className="space-y-3 rounded border border-stone-200 bg-white p-4" data-testid="preset">
            <header className="flex items-start justify-between gap-2">
              <div>
                <h2 className="font-semibold">{p.name}</h2>
                {p.description && <p className="text-sm text-stone-600">{p.description}</p>}
              </div>
              {p.builtIn && <span className="rounded bg-stone-100 px-2 py-0.5 text-xs text-stone-600">Built-in</span>}
            </header>
            <dl className="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-1 text-sm">
              {FIELDS.filter(([key]) => p[key]).map(([key, label]) => (
                <div key={key} className="contents">
                  <dt className="text-stone-500">{label}</dt>
                  <dd>{p[key]}</dd>
                </div>
              ))}
            </dl>
          </li>
        ))}
      </ul>
    </section>
  )
}
