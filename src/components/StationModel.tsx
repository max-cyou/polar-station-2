import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { GLTFLoader } from "three/addons/loaders/GLTFLoader.js";

function disposeModel(root: THREE.Object3D) {
  const textures = new Set<THREE.Texture>();
  const materials = new Set<THREE.Material>();
  const geometries = new Set<THREE.BufferGeometry>();
  root.traverse((object) => {
    if (!(object instanceof THREE.Mesh)) return;
    geometries.add(object.geometry);
    for (const material of Array.isArray(object.material) ? object.material : [object.material]) {
      materials.add(material);
      for (const value of Object.values(material)) if (value instanceof THREE.Texture) textures.add(value);
    }
  });
  textures.forEach(t => t.dispose());
  materials.forEach(m => m.dispose());
  geometries.forEach(g => g.dispose());
}

export function StationModel() {
  const container = useRef<HTMLDivElement>(null);
  const [fallback, setFallback] = useState(false);

  useEffect(() => {
    const host = container.current!;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: "low-power" });
    } catch {
      setFallback(true);
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setClearColor(0, 0);
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = 1.2;
    host.appendChild(renderer.domElement);
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(31, 1, .01, 5);
    camera.position.set(.28, .255, .37);
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(0, .101, 0);
    controls.enablePan = false;
    controls.enableZoom = false;
    controls.enableDamping = true;
    controls.dampingFactor = .1;
    controls.minPolarAngle = .12;
    controls.maxPolarAngle = Math.PI - .12;
    const ambient = new THREE.HemisphereLight(0xe4ebff, 0x43414c, 2.3);
    scene.add(ambient);
    const key = new THREE.DirectionalLight(0xfff5ef, 4);
    key.position.set(-.2, .5, .3);
    scene.add(key);
    const rim = new THREE.DirectionalLight(0x9aaeff, 2.7);
    rim.position.set(.3, .3, -.25);
    scene.add(rim);
    const fill = new THREE.DirectionalLight(0xc9e2ff, 1.1);
    fill.position.set(.1, .08, .4);
    scene.add(fill);

    let disposed = false;
    let frame = 0;
    let model: THREE.Object3D | undefined;
    const render = () => {
      frame = 0;
      if (disposed) return;
      controls.update();
      renderer.render(scene, camera);
    };
    const invalidate = () => {
      if (!disposed && !frame) frame = requestAnimationFrame(render);
    };
    controls.addEventListener("change", invalidate);
    const resize = () => {
      const { width, height } = host.getBoundingClientRect();
      if (!width || !height) return;
      renderer.setSize(width, height, false);
      camera.aspect = width / height;
      camera.updateProjectionMatrix();
      invalidate();
    };
    const observer = new ResizeObserver(resize);
    observer.observe(host);
    resize();
    new GLTFLoader().load("/models/station2.glb", (gltf) => {
      if (disposed) { disposeModel(gltf.scene); return; }
      model = gltf.scene;
      scene.add(model);
      host.dataset.loaded = "true";
      invalidate();
    }, undefined, () => { if (!disposed) setFallback(true); });
    const onContextLost = (event: Event) => {
      event.preventDefault();
      if (!disposed) setFallback(true);
    };
    renderer.domElement.addEventListener("webglcontextlost", onContextLost);
    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      controls.dispose();
      if (model) disposeModel(model);
      renderer.domElement.removeEventListener("webglcontextlost", onContextLost);
      renderer.dispose();
      renderer.domElement.remove();
      delete host.dataset.loaded;
    };
  }, []);

  return <div className="station-model" ref={container} role="img" aria-label="3D-модель Яндекс Станции 2. Вращайте перетаскиванием мышью или пальцем.">
    {fallback && <img className="station-image" src="/station.webp" alt="Яндекс Станция 2" />}
  </div>;
}
