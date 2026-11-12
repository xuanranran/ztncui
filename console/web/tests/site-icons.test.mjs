import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const pngSignature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);

function checkPng(data, size) {
  assert.deepEqual(data.subarray(0, 8), pngSignature);
  assert.equal(data.toString('ascii', 12, 16), 'IHDR');
  assert.equal(data.readUInt32BE(16), size);
  assert.equal(data.readUInt32BE(20), size);
  assert.equal(data[25], 6, 'Icon PNGs must retain RGBA transparency');
}

test('website icon links point to correctly sized local images', () => {
  const html = readFileSync(new URL('index.html', root), 'utf8');
  for (const [name, size] of [['favicon-32x32.png', 32], ['icon-192.png', 192], ['apple-touch-icon.png', 180]]) {
    assert.ok(html.includes(`href="/${name}?v=1"`));
    checkPng(readFileSync(new URL(`public/${name}`, root)), size);
  }
  assert.ok(html.includes('rel="apple-touch-icon"'));
  assert.ok(html.includes('href="/favicon.ico?v=1"'));
});

test('favicon ICO contains all six browser sizes with valid PNG entries', () => {
  const data = readFileSync(new URL('public/favicon.ico', root));
  const sizes = [16, 32, 48, 64, 128, 256];
  assert.equal(data.readUInt16LE(0), 0);
  assert.equal(data.readUInt16LE(2), 1);
  assert.equal(data.readUInt16LE(4), sizes.length);
  let nextOffset = 6 + 16 * sizes.length;
  sizes.forEach((size, i) => {
    const entry = 6 + 16 * i;
    assert.equal(data[entry] || 256, size);
    assert.equal(data[entry + 1] || 256, size);
    assert.equal(data.readUInt16LE(entry + 6), 32);
    const length = data.readUInt32LE(entry + 8);
    const offset = data.readUInt32LE(entry + 12);
    assert.equal(offset, nextOffset);
    assert.ok(offset + length <= data.length);
    checkPng(data.subarray(offset, offset + length), size);
    nextOffset += length;
  });
  assert.equal(nextOffset, data.length);
});
