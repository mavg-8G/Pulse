import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, join, resolve } from 'node:path'
import { publishImage } from './publish-image.mjs'

function fixture(t, { layout = 'classic', remoteConfig, remoteDigest, tags, savedManifest } = {}) {
  const root = resolve(tmpdir())
  const temp = mkdtempSync(join(root, 'pulse-publish-test-'))
  t.after(() => {
    assert.equal(dirname(temp), root)
    assert.ok(basename(temp).startsWith('pulse-publish-test-'))
    rmSync(temp, { recursive: true, force: true })
  })
  const config = Buffer.from('{"architecture":"amd64","rootfs":{"type":"layers","diff_ids":["sha256:tested-layer"]}}\n')
  const configHash = createHash('sha256').update(config).digest('hex')
  const manifestDigest = `sha256:${'b'.repeat(64)}`
  const configPath = layout === 'classic' ? `${configHash}.json` : `blobs/sha256/${configHash}`
  const image = 'ghcr.io/mavg-8g/pulse'
  const revision = 'a'.repeat(40)
  const output = join(temp, 'outputs')
  const calls = []
  const env = { IMAGE: image, GITHUB_SHA: revision, GITHUB_OUTPUT: output, RUNNER_TEMP: temp, RELEASE_TAGS: tags ?? `${image}:main\n${image}:${revision}` }
  const run = (name, args) => {
    calls.push([name, ...args])
    if (name === 'tar') {
      if (args[2] === 'manifest.json') return Buffer.from(JSON.stringify(savedManifest ?? [{ Config: configPath }]))
      assert.equal(args[2], configPath)
      return config
    }
    assert.equal(name, 'docker')
    if (args.includes('--format')) return Buffer.from(JSON.stringify(remoteDigest ?? manifestDigest))
    if (args.includes('--raw')) return Buffer.from(JSON.stringify({ config: { digest: remoteConfig ?? `sha256:${configHash}` } }))
    if (args[0] === 'image' || args[0] === 'tag' || args[0] === 'push') return Buffer.from('')
    throw new Error(`Unexpected command: ${args.join(' ')}`)
  }
  return { env, run, calls, output, temp, manifestDigest }
}

for (const layout of ['classic', 'oci']) {
  test(`publishes and verifies ${layout} archive without relying on Docker .Id`, (t) => {
    const f = fixture(t, { layout })
    assert.equal(publishImage({ ...f, log: () => {} }), f.manifestDigest)
    assert.equal(readFileSync(f.output, 'utf8'), `digest=${f.manifestDigest}\n`)
    assert.equal(f.calls.filter((c) => c[0] === 'docker' && c[1] === 'push').length, 2)
    assert.ok(!f.calls.some((c) => c.includes('{{.Id}}') || c.includes('{{index .RepoDigests 0}}')))
    assert.deepEqual(readdirSync(f.temp), ['outputs'])
  })
}

test('rejects a substituted remote config and never exposes a deploy digest', (t) => {
  const f = fixture(t, { remoteConfig: `sha256:${'c'.repeat(64)}` })
  assert.throws(() => publishImage({ ...f, log: () => {} }), /no coincide/)
  assert.equal(existsSync(f.output), false)
  assert.deepEqual(readdirSync(f.temp), [])
})

test('rejects a malformed registry digest', (t) => {
  const f = fixture(t, { remoteDigest: 'not-a-digest' })
  assert.throws(() => publishImage({ ...f, log: () => {} }), /digest de manifiesto/)
  assert.equal(existsSync(f.output), false)
})

test('never pushes tags outside the intended repository', (t) => {
  const f = fixture(t, { tags: `other/image:main\nghcr.io/mavg-8g/pulse:${'a'.repeat(40)}` })
  assert.throws(() => publishImage({ ...f, log: () => {} }), /etiquetas/)
  assert.deepEqual(f.calls, [])
})

test('rejects ambiguous archives and traversal paths', (t) => {
  for (const savedManifest of [[{ Config: '../../secret' }], [{ Config: 'one' }, { Config: 'two' }]]) {
    const f = fixture(t, { savedManifest })
    assert.throws(() => publishImage({ ...f, log: () => {} }), /configuración inesperada|exactamente una imagen/)
    assert.ok(!f.calls.some((c) => c[1] === 'push'))
    assert.equal(existsSync(f.output), false)
  }
})

test('propagates Docker failures instead of continuing to sign/deploy', (t) => {
  const f = fixture(t)
  const run = (name, args) => {
    if (name === 'docker' && args[0] === 'push') throw new Error('docker push failed')
    return f.run(name, args)
  }
  assert.throws(() => publishImage({ ...f, run, log: () => {} }), /docker push failed/)
  assert.equal(existsSync(f.output), false)
  assert.deepEqual(readdirSync(f.temp), [])
})
