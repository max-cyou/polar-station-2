import * as THREE from "three";

// Original visual reconstruction from product photographs, in metres.
// Dimensions: https://alice.yandex.ru/support/ru/station/meet/characteristics-gen2
// Detail references: https://habr.com/ru/articles/669924/
// Proportions of small parts are estimated from the photographs.
const W = 0.123;
const D = 0.099;
const R = 0.017;

function roundedShape(w: number, d: number, r: number) {
  const s = new THREE.Shape();
  const x = -w / 2, z = -d / 2;
  s.moveTo(x + r, z);
  s.lineTo(x + w - r, z);
  s.quadraticCurveTo(x + w, z, x + w, z + r);
  s.lineTo(x + w, z + d - r);
  s.quadraticCurveTo(x + w, z + d, x + w - r, z + d);
  s.lineTo(x + r, z + d);
  s.quadraticCurveTo(x, z + d, x, z + d - r);
  s.lineTo(x, z + r);
  s.quadraticCurveTo(x, z, x + r, z);
  return s;
}

function slab(w: number, d: number, r: number, h: number, bevel: number) {
  const geometry = new THREE.ExtrudeGeometry(roundedShape(w - 2 * bevel, d - 2 * bevel, r - bevel), {
    depth: h - 2 * bevel,
    bevelEnabled: bevel > 0,
    bevelSegments: 3,
    steps: 1,
    bevelSize: bevel,
    bevelThickness: bevel,
    curveSegments: 12,
  });
  geometry.rotateX(-Math.PI / 2);
  geometry.translate(0, bevel, 0);
  return geometry;
}

function topFace(w: number, d: number, r: number) {
  const geometry = new THREE.ShapeGeometry(roundedShape(w, d, r), 16);
  const pos = geometry.getAttribute("position");
  const uv = geometry.getAttribute("uv");
  for (let i = 0; i < pos.count; i++) uv.setXY(i, pos.getX(i) / w + .5, pos.getY(i) / d + .5);
  geometry.rotateX(-Math.PI / 2);
  return geometry;
}

function canvas(size: number) {
  const element = document.createElement("canvas");
  element.width = element.height = size;
  return { element, ctx: element.getContext("2d")! };
}

function fabricMaps() {
  const { element, ctx } = canvas(512);
  ctx.fillStyle = "#171c22";
  ctx.fillRect(0, 0, 512, 512);
  // Seamless interlocking yarn loops; all texture pixels are procedural.
  for (let y = -24; y < 536; y += 16) {
    for (let x = -24; x < 536; x += 16) {
      const offset = ((y / 16) & 1) * 8;
      const xx = x + offset;
      ctx.beginPath();
      ctx.moveTo(xx - 5, y - 5);
      ctx.bezierCurveTo(xx - 9, y + 1, xx - 6, y + 8, xx, y + 10);
      ctx.bezierCurveTo(xx + 6, y + 6, xx + 8, y + 1, xx + 5, y - 5);
      ctx.strokeStyle = "#101419"; ctx.lineWidth = 7; ctx.stroke();
      ctx.strokeStyle = "#505a65"; ctx.lineWidth = 4.5; ctx.stroke();
      ctx.strokeStyle = "#78818a"; ctx.lineWidth = 1.25; ctx.stroke();
    }
  }
  // Fine stray fibres break up the smooth yarn surface.
  let fibreSeed = 2701;
  const random = () => {
    fibreSeed = (Math.imul(fibreSeed, 1664525) + 1013904223) >>> 0;
    return fibreSeed / 4294967296;
  };
  ctx.lineWidth = .55;
  for (let i = 0; i < 6500; i++) {
    const x = random() * 512, y = random() * 512;
    const angle = random() * Math.PI * 2, length = 2 + random() * 5;
    ctx.strokeStyle = random() > .5 ? "#b2b8be45" : "#080c1055";
    ctx.beginPath();
    ctx.moveTo(x, y);
    ctx.quadraticCurveTo(x + Math.cos(angle + .4) * length * .6, y + Math.sin(angle + .4) * length * .6, x + Math.cos(angle) * length, y + Math.sin(angle) * length);
    ctx.stroke();
  }
  const pixels = ctx.getImageData(0, 0, 512, 512);
  let seed = 1701;
  for (let i = 0; i < pixels.data.length; i += 4) {
    seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
    const noise = ((seed >>> 24) / 255 - .5) * 10;
    for (let c = 0; c < 3; c++) pixels.data[i + c] += noise;
  }
  ctx.putImageData(pixels, 0, 0);
  const normal = canvas(512);
  const normalPixels = normal.ctx.createImageData(512, 512);
  const sample = (x: number, y: number) => pixels.data[(((y + 512) % 512) * 512 + (x + 512) % 512) * 4] / 255;
  for (let y = 0; y < 512; y++) for (let x = 0; x < 512; x++) {
    const v = new THREE.Vector3((sample(x - 1, y) - sample(x + 1, y)) * 2, (sample(x, y - 1) - sample(x, y + 1)) * 2, 1).normalize();
    const i = (y * 512 + x) * 4;
    normalPixels.data.set([(v.x * .5 + .5) * 255, (v.y * .5 + .5) * 255, (v.z * .5 + .5) * 255, 255], i);
  }
  normal.ctx.putImageData(normalPixels, 0, 0);
  const map = new THREE.CanvasTexture(element);
  const normalMap = new THREE.CanvasTexture(normal.element);
  map.colorSpace = THREE.SRGBColorSpace;
  for (const texture of [map, normalMap]) {
    texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
    texture.repeat.set(4, 2);
    texture.anisotropy = 8;
  }
  return { map, normalMap };
}

function fabricBody() {
  const outline = roundedShape(W, D, R).getPoints(16);
  const distances = [0];
  for (let i = 1; i < outline.length; i++) distances.push(distances[i - 1] + outline[i].distanceTo(outline[i - 1]));
  const total = distances[distances.length - 1];
  const levels = [[.003, .96], [.006, .993], [.010, 1], [.188, 1], [.193, .996], [.195, .983]];
  const vertices: number[] = [], uv: number[] = [], indices: number[] = [];
  levels.forEach(([height, scale]) => outline.forEach((p, i) => {
    vertices.push(p.x * scale, height, -p.y * scale);
    uv.push(distances[i] / total, height / .195);
  }));
  const n = outline.length;
  for (let j = 0; j < levels.length - 1; j++) for (let i = 0; i < n - 1; i++) {
    const a = j * n + i, b = a + n;
    indices.push(a, a + 1, b, a + 1, b + 1, b);
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.Float32BufferAttribute(vertices, 3));
  geometry.setAttribute("uv", new THREE.Float32BufferAttribute(uv, 2));
  geometry.setIndex(indices);
  geometry.computeVertexNormals();
  geometry.computeTangents();
  return geometry;
}

function panelTexture() {
  const { element, ctx } = canvas(512);
  ctx.fillStyle = "#11101c";
  ctx.fillRect(0, 0, 512, 512);
  const glows = [[130, 240, "#6b27e8"], [370, 280, "#5935dc"], [240, 315, "#b35af3"], [280, 180, "#46209b"]] as const;
  ctx.globalCompositeOperation = "screen";
  for (const [x, y, color] of glows) {
    const gradient = ctx.createRadialGradient(x, y, 0, x, y, 230);
    gradient.addColorStop(0, color); gradient.addColorStop(1, "#000000");
    ctx.fillStyle = gradient; ctx.fillRect(0, 0, 512, 512);
  }
  const shade = ctx.createRadialGradient(256, 256, 110, 256, 256, 315);
  shade.addColorStop(0, "#00000000"); shade.addColorStop(1, "#000000d9");
  ctx.globalCompositeOperation = "source-over";
  ctx.fillStyle = shade; ctx.fillRect(0, 0, 512, 512);
  const texture = new THREE.CanvasTexture(element);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

export function createStation2(light: "off" | "purple" = "off") {
  const group = new THREE.Group();
  group.name = `Yandex_Station_2_Anthracite_${light}`;
  group.userData = { dimensionsMillimeters: [123, 199, 99], reconstruction: "Approximate original model from photographs", reference: "https://habr.com/ru/articles/669924/" };
  const plastic = new THREE.MeshStandardMaterial({ color: "#14171d", roughness: .73, metalness: .08 });
  const rubber = new THREE.MeshStandardMaterial({ color: "#080a0d", roughness: .95 });
  const silver = new THREE.MeshStandardMaterial({ color: "#9ca5b0", roughness: .5, metalness: .4 });
  const markings = new THREE.MeshStandardMaterial({ color: "#a8a7bd", roughness: .9, emissive: "#494257", emissiveIntensity: .15 });
  function add(name: string, geometry: THREE.BufferGeometry, material: THREE.Material, x = 0, y = 0, z = 0) {
    const mesh = new THREE.Mesh(geometry, material);
    mesh.name = name; mesh.position.set(x, y, z); mesh.castShadow = true; mesh.receiveShadow = true;
    group.add(mesh); return mesh;
  }
  add("Fabric_body", fabricBody(), new THREE.MeshPhysicalMaterial({ ...fabricMaps(), color: "#44474c", roughness: .98, normalScale: new THREE.Vector2(1.55, 1.55), sheen: .1, sheenColor: new THREE.Color("#3b3e44"), sheenRoughness: .95 }));
  add("Recessed_rubber_base", slab(.115, .091, .015, .004, .0007), rubber);
  add("Top_bezel", slab(W, D, R, .0035, .0006), plastic, 0, .195, 0);
  const glow = light === "purple" ? panelTexture() : null;
  const diffuser = glow
    ? new THREE.MeshStandardMaterial({ map: glow, emissiveMap: glow, emissive: "#ffffff", emissiveIntensity: .75, roughness: .55, metalness: .05 })
    : new THREE.MeshStandardMaterial({ color: "#1b1e25", roughness: .72, metalness: .05 });
  add("LED_diffuser", topFace(.1216, .0976, .0165), diffuser, 0, .199, 0);
  add("Touch_controls", slab(.072, .021, .0105, .00035, .00006), new THREE.MeshStandardMaterial({ color: "#10121b", roughness: .64 }), 0, .1991, 0);
  const minus = new THREE.BoxGeometry(.0038, .00011, .00045);
  add("Volume_minus", minus, markings, -.024, .1996, 0);
  add("Volume_plus_horizontal", minus.clone(), markings, .024, .1996, 0);
  add("Volume_plus_vertical", new THREE.BoxGeometry(.00045, .00011, .0038), markings, .024, .1996, 0);
  const alice = new THREE.Shape();
  alice.absarc(0, 0, .0055, 0, Math.PI * 2, false);
  const hole = new THREE.Path();
  hole.moveTo(0, .0038);
  hole.bezierCurveTo(.0015, .0028, .0035, -.0012, .0024, -.0022);
  hole.bezierCurveTo(.0017, -.003, -.0017, -.003, -.0024, -.0022);
  hole.bezierCurveTo(-.0035, -.0012, -.0015, .0028, 0, .0038);
  alice.holes.push(hole);
  const aliceGeo = new THREE.ShapeGeometry(alice, 24); aliceGeo.rotateX(-Math.PI / 2);
  add("Alice_touch_symbol", aliceGeo, light === "purple"
    ? new THREE.MeshStandardMaterial({ color: "#b692f8", emissive: "#7548dc", emissiveIntensity: .85, roughness: .6 })
    : new THREE.MeshStandardMaterial({ color: "#292d35", roughness: .72 }), 0, .1997, 0);
  for (const x of [-.014, .014]) for (const z of [-.0062, .0062]) {
    const geo = new THREE.CircleGeometry(.00048, 12); geo.rotateX(-Math.PI / 2);
    add(`Microphone_${x}_${z}`, geo, rubber, x, .1996, z);
  }
  const mute = add("Microphone_switch_surround", slab(.020, .0072, .0036, .0011, .0002), rubber);
  mute.rotation.x = Math.PI / 2; mute.position.set(0, .167, -.0497);
  const muteKey = add("Microphone_switch", slab(.015, .0044, .0022, .0012, .0002), plastic);
  muteKey.rotation.x = Math.PI / 2; muteKey.position.set(0, .168, -.0501);
  // Tiny crossed microphone marking on the rear physical button.
  add("Microphone_mark", new THREE.CapsuleGeometry(.0005, .0014, 3, 6), silver, 0, .1677, -.0514);
  const slash = add("Microphone_slash", new THREE.BoxGeometry(.0002, .0037, .00015), silver, 0, .1677, -.0519);
  slash.rotation.z = -.65;
  const port = add("USB_C_recess", slab(.014, .007, .0035, .0012, .0003), rubber);
  port.rotation.x = Math.PI / 2; port.position.set(0, .015, -.0493);
  const socket = add("USB_C_metal", slab(.009, .0035, .0017, .0008, .0001), silver);
  socket.rotation.x = Math.PI / 2; socket.position.set(0, .015, -.0501);
  const socketInside = add("USB_C_socket", slab(.0076, .0023, .0011, .0009, .00008), rubber);
  socketInside.rotation.x = Math.PI / 2; socketInside.position.set(0, .015, -.0504);
  return group;
}
