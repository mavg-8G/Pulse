import { useQuery } from '@tanstack/react-query'
import { get } from '../lib/api'
import { versionLabel, type ServerVersion } from '../lib/version'

export function BuildVersion({ tz }: { tz: string }) {
  const version = useQuery({
    queryKey: ['server-version'],
    queryFn: () => get<ServerVersion>('/api/version'),
    staleTime: 0,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    retry: 1,
  })
  const data = version.data
  const label = data ? versionLabel(data.built_at, tz) : null
  const unverified = version.isError || version.fetchStatus === 'paused'
  const description = data
    ? `Versión del servidor · ${tz}${data.built_at ? ` · UTC: ${data.built_at}` : ''}${data.revision ? ` · Commit: ${data.revision}` : ''}`
    : 'Consultar versión del servidor'
  return (
    <span className="build-version" data-testid="server-version" title={description}>
      {label ? <time dateTime={data!.built_at}>{label}</time> : data ? 'Versión de desarrollo' : version.isPending && !unverified ? 'Consultando versión…' : 'Versión no disponible'}
      {data?.revision && <span> · {data.revision.slice(0, 7)}</span>}
      {label && unverified && <span> · sin verificar</span>}
    </span>
  )
}
