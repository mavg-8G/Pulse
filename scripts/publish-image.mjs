// Publish the tested local image and verify its config bytes, independently of
// whether Docker's local .Id denotes a config, manifest or index.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { appendFileSync, mkdtempSync, rmSync } from 'node:fs'
import { basename, dirname, join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

const digestPattern = /^sha256:[a-f0-9]{64}$/
const command = (name, args) => execFileSync(name, args, { maxBuffer: 16 * 1024 * 1024, stdio: ['ignore', 'pipe', 'inherit'] })

export function publishImage({ env = process.env, run = command, log = console.log } = {}) {
  const { IMAGE: image, GITHUB_SHA: revision, RELEASE_TAGS: rawTags, GITHUB_OUTPUT: output, RUNNER_TEMP: runnerTemp } = env
  if (!image || !/^[a-f0-9]{40}$/.test(revision ?? '') || !output || !runnerTemp) throw new Error('Falta configuración de publicación válida.')
  const tags = [...new Set((rawTags ?? '').split(/\r?\n/).filter(Boolean))]
  const commitTag = `${image}:${revision}`
  if (!tags.includes(commitTag) || tags.some((tag) => !tag.startsWith(`${image}:`) || !/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/.test(tag.slice(image.length + 1)))) {
    throw new Error('Las etiquetas deben pertenecer a IMAGE e incluir el commit completo.')
  }

  const tempRoot = resolve(runnerTemp)
  const workdir = mkdtempSync(join(tempRoot, 'pulse-publish-'))
  try {
    const archive = join(workdir, 'tested.tar')
    run('docker', ['image', 'save', '--output', archive, 'pulse:ci'])
    const saved = JSON.parse(run('tar', ['-xOf', archive, 'manifest.json']).toString())
    if (!Array.isArray(saved) || saved.length !== 1) throw new Error('La exportación debe contener exactamente una imagen probada.')
    const configPath = saved[0].Config
    // Docker save uses either a classic config.json or an OCI blob path.
    if (typeof configPath !== 'string' || !/^(?:[a-f0-9]{64}\.json|blobs\/sha256\/[a-f0-9]{64})$/.test(configPath)) {
      throw new Error('Ruta de configuración inesperada en la imagen exportada.')
    }
    const config = run('tar', ['-xOf', archive, configPath])
    const testedConfig = `sha256:${createHash('sha256').update(config).digest('hex')}`
    log(`Configuración de la imagen probada: ${testedConfig}`)

    for (const tag of tags) {
      run('docker', ['tag', 'pulse:ci', tag])
      const pushed = run('docker', ['push', tag])
      if (pushed.length) log(pushed.toString())
    }

    const digest = JSON.parse(run('docker', ['buildx', 'imagetools', 'inspect', commitTag, '--format', '{{json .Manifest.Digest}}']).toString())
    if (typeof digest !== 'string' || !digestPattern.test(digest)) throw new Error('GHCR no devolvió un digest de manifiesto válido.')
    const remote = JSON.parse(run('docker', ['buildx', 'imagetools', 'inspect', '--raw', `${image}@${digest}`]).toString())
    const remoteConfig = remote.config?.digest
    if (typeof remoteConfig !== 'string' || !digestPattern.test(remoteConfig)) throw new Error('El manifiesto remoto no contiene una configuración de imagen válida.')
    if (remoteConfig !== testedConfig) throw new Error(`La imagen publicada no coincide con la probada: configuración local=${testedConfig}, remota=${remoteConfig}.`)

    appendFileSync(output, `digest=${digest}\n`)
    log(`Imagen publicada y verificada: ${image}@${digest}`)
    return digest
  } finally {
    // Delete only the directory created for this invocation, under RUNNER_TEMP.
    if (dirname(workdir) === tempRoot && basename(workdir).startsWith('pulse-publish-')) rmSync(workdir, { recursive: true, force: true })
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    publishImage()
  } catch (error) {
    console.error(`No se pudo publicar/verificar la imagen: ${error.message}`)
    process.exitCode = 1
  }
}
