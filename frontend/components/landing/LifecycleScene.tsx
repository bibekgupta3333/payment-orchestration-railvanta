"use client";

import { useEffect, useRef } from "react";
import {
  NODES,
  VIEW_HALF,
  frameAt,
  staticFrame,
  type Frame,
  type Tone,
  type UseCaseId,
} from "./lifecycle-path";

const HALF = NODES.length / 2;
const TILT = 0.62; // camera elevation in radians; it never moves sideways
const FOV = 30;
const TARGET_Y = 0.55;
const HOVER = 1.2; // token height above the track
const TRAIL = 6;
const TRAIL_STEP = 0.07; // seconds between trail samples
const TONES: Record<Tone, string | undefined> = {
  normal: undefined, // theme accent
  alert: "#ff4d4f",
  replay: "#f5a524",
  ok: "#2fd17a",
};
/** Plate footprint and height per node; Provider and Ledger are larger. */
// Footprints stay below the 1-unit node spacing (even at the 1.08 active scale),
// so neighbours never touch; Provider and Ledger remain the largest.
const SIZE = [0.6, 0.66, 0.6, 0.7, 0.84, 0.84, 0.66];
const HEIGHT = [0.4, 0.46, 0.4, 0.52, 0.84, 0.84, 0.46];

const worldX = (x: number) => x + 0.5 - HALF;

function cssColor(el: HTMLElement, name: string, fallback: string): string {
  return getComputedStyle(el).getPropertyValue(name).trim() || fallback;
}

/**
 * The three.js scene: lit rounded plates on a base, a track, and a glowing
 * token with a fading trail. three is imported only after mount, so it is
 * never in the initial bundle or the server render. Text stays in HTML.
 */
export function LifecycleScene({
  useCase,
  playing,
  reducedMotion,
  onFrame,
  onUnavailable,
}: {
  useCase: UseCaseId;
  playing: boolean;
  reducedMotion: boolean;
  onFrame: (frame: Frame) => void;
  onUnavailable: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  // Latest props for the long-lived render loop, which is created once.
  const live = useRef({ useCase, playing, reducedMotion, onFrame });
  const nudge = useRef<() => void>(() => undefined);

  useEffect(() => {
    live.current = { useCase, playing, reducedMotion, onFrame };
    nudge.current();
  }, [useCase, playing, reducedMotion, onFrame]);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let disposed = false;
    let cleanup: () => void = () => undefined;

    void Promise.all([
      import("three"),
      import("three/examples/jsm/geometries/RoundedBoxGeometry.js"),
    ])
      .then(([THREE, { RoundedBoxGeometry }]) => {
        if (disposed) return;
        let renderer: InstanceType<typeof THREE.WebGLRenderer>;
        try {
          renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
        } catch {
          onUnavailable();
          return;
        }
        renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
        el.appendChild(renderer.domElement);
        const scene = new THREE.Scene();
        const camera = new THREE.PerspectiveCamera(FOV, 1.6, 0.1, 60);
        scene.add(new THREE.AmbientLight(0xffffff, 1.4));
        const sun = new THREE.DirectionalLight(0xffffff, 2.2);
        sun.position.set(-3, 6, 5);
        scene.add(sun);

        type Mat = InstanceType<typeof THREE.MeshStandardMaterial>;
        const geometries: { dispose(): void }[] = [];
        const materials: Mat[] = [];
        const geo = <G extends { dispose(): void }>(g: G): G => {
          geometries.push(g);
          return g;
        };
        const mat = (opacity = 1): Mat => {
          const m = new THREE.MeshStandardMaterial({
            roughness: 0.45,
            metalness: 0.1,
            transparent: opacity < 1,
            opacity,
          });
          materials.push(m);
          return m;
        };
        const put = (
          g: ConstructorParameters<typeof THREE.Mesh>[0] & object,
          m: Mat,
        ) => {
          const mesh = new THREE.Mesh(g, m);
          scene.add(mesh);
          return mesh;
        };

        const base = put(
          geo(new RoundedBoxGeometry(NODES.length + 0.5, 0.12, 1.9, 3, 0.05)),
          mat(),
        );
        base.position.y = -0.1;
        const grid = new THREE.GridHelper(14, 28, 0x888888, 0x888888);
        grid.position.y = -0.17;
        scene.add(grid);
        const track = put(
          geo(new THREE.CylinderGeometry(0.05, 0.05, NODES.length - 1, 12)),
          mat(),
        );
        track.rotation.z = Math.PI / 2;
        track.position.y = 0.12;
        const plates = NODES.map((_, i) => {
          const plate = put(
            geo(new RoundedBoxGeometry(SIZE[i], HEIGHT[i], SIZE[i], 4, 0.07)),
            mat(),
          );
          plate.position.set(worldX(i), HEIGHT[i] / 2 - 0.04, 0);
          return plate;
        });

        // Tokens: emissive core, soft halo and a trail of shrinking beads.
        const coreGeo = geo(new THREE.SphereGeometry(0.22, 24, 16));
        const haloGeo = geo(new THREE.SphereGeometry(0.44, 24, 16));
        const tokens = [0, 1].map(() => {
          const core = put(coreGeo, mat());
          const halo = put(haloGeo, mat(0.28));
          const trail = Array.from({ length: TRAIL }, (_, k) => ({
            mesh: put(coreGeo, mat(0.5 * (1 - k / TRAIL))),
            scale: 0.85 * (1 - k / (TRAIL + 1)),
          }));
          return { core, halo, trail };
        });

        // Lookup arrow: out of the Unknown marker and back onto the Provider.
        const px = worldX(4);
        const arc = new THREE.CubicBezierCurve3(
          new THREE.Vector3(px, 2.1, 0),
          new THREE.Vector3(px + 1.7, 2.3, 0),
          new THREE.Vector3(px + 1.4, 1.2, 0),
          new THREE.Vector3(px + 0.1, 1.0, 0),
        );
        const arrowMat = mat();
        const arrow = put(
          geo(new THREE.TubeGeometry(arc, 28, 0.03, 8)),
          arrowMat,
        );
        const head = put(geo(new THREE.ConeGeometry(0.1, 0.24, 14)), arrowMat);
        head.position.set(px + 0.1, 1.0, 0);
        head.rotation.z = Math.atan2(-(px + 0.1 - (px + 1.4)), 1.0 - 1.2);

        const paint = (frame: Frame, time: number, trailOn: boolean) => {
          const accent = cssColor(el, "--accent", "#08776f");
          const muted = cssColor(el, "--muted", "#536378");
          const line = cssColor(el, "--line", "#dce3ec");
          const tint = (m: Mat, color: string, glow = 0) => {
            m.color.set(color);
            m.emissive.set(glow ? color : "#000000");
            m.emissiveIntensity = glow;
          };
          tint(base.material as Mat, line);
          for (const m of [grid.material].flat()) {
            m.transparent = true;
            m.opacity = 0.35;
            m.color.set(muted);
          }
          tint(track.material as Mat, muted, 0.15);
          plates.forEach((plate, i) => {
            const active = frame.step === i;
            tint(
              plate.material as Mat,
              active ? accent : muted,
              active ? 0.45 : 0,
            );
            plate.scale.setScalar(active ? 1.08 : 1);
          });
          const lookup =
            frame.marker === "unknown" || frame.marker === "lookup";
          arrow.visible = head.visible = lookup && trailOn;
          tint(arrowMat, accent, 0.6);
          tokens.forEach((token, j) => {
            const state = frame.tokens[j];
            token.core.visible = token.halo.visible = Boolean(state);
            token.trail.forEach(({ mesh }) => (mesh.visible = false));
            if (!state) return;
            const color = TONES[state.tone] ?? accent;
            const pulse =
              state.tone === "alert" ? 1 + 0.3 * Math.sin(time * 14) : 1;
            const at = (
              s: { x: number; y: number },
              target: {
                position: { set(x: number, y: number, z: number): void };
              },
            ) => target.position.set(worldX(s.x), HOVER + s.y * 0.85, 0.05);
            at(state, token.core);
            at(state, token.halo);
            token.core.scale.setScalar(pulse);
            token.halo.scale.setScalar(pulse * (1 + 0.1 * Math.sin(time * 5)));
            tint(token.core.material as Mat, color, 2.2);
            tint(token.halo.material as Mat, color, 1);
            token.trail.forEach(({ mesh, scale }, k) => {
              const lag = time - (k + 1) * TRAIL_STEP;
              const past =
                trailOn && lag >= 0
                  ? frameAt(live.current.useCase, lag).tokens[j]
                  : undefined;
              mesh.visible = Boolean(past);
              if (!past) return;
              at(past, mesh);
              mesh.scale.setScalar(scale);
              tint(mesh.material as Mat, color, 1.5);
            });
          });
        };

        const resize = () => {
          const width = Math.max(el.clientWidth, 1);
          const height = Math.max(el.clientHeight, 1);
          renderer.setSize(width, height, false);
          camera.aspect = width / height;
          // Depth at which half the view width equals VIEW_HALF node units.
          const depth =
            VIEW_HALF / (Math.tan((FOV * Math.PI) / 360) * camera.aspect);
          const dist = depth - TARGET_Y * Math.sin(TILT);
          camera.position.set(
            0,
            TARGET_Y + dist * Math.sin(TILT),
            dist * Math.cos(TILT),
          );
          camera.lookAt(0, TARGET_Y, 0);
          camera.updateProjectionMatrix();
        };

        let time = 0;
        let last = 0;
        let raf = 0;
        let visible = true;
        let current: Frame | undefined;
        let shownUseCase = live.current.useCase;

        const draw = () => {
          const {
            useCase: id,
            reducedMotion: still,
            onFrame: emit,
          } = live.current;
          if (id !== shownUseCase) {
            shownUseCase = id;
            time = 0;
          }
          const frame = still ? staticFrame(id) : frameAt(id, time);
          paint(frame, time, !still);
          renderer.render(scene, camera);
          if (
            !current ||
            current.step !== frame.step ||
            current.marker !== frame.marker
          ) {
            emit(frame);
          }
          current = frame;
        };
        const running = () =>
          live.current.playing &&
          !live.current.reducedMotion &&
          visible &&
          !document.hidden;
        const tick = (now: number) => {
          raf = 0;
          if (disposed || !running()) return;
          time += Math.min((now - last) / 1000, 0.1);
          last = now;
          draw();
          raf = requestAnimationFrame(tick);
        };
        const sync = () => {
          if (running()) {
            if (!raf) {
              last = performance.now();
              raf = requestAnimationFrame(tick);
            }
          } else {
            if (raf) cancelAnimationFrame(raf);
            raf = 0;
            draw();
          }
        };
        nudge.current = sync;

        resize();
        const resizeObserver = new ResizeObserver(() => {
          resize();
          draw();
        });
        resizeObserver.observe(el);
        const intersection = new IntersectionObserver(([entry]) => {
          visible = entry?.isIntersecting ?? true;
          sync();
        });
        intersection.observe(el);
        const theme = new MutationObserver(draw);
        theme.observe(document.documentElement, {
          attributes: true,
          attributeFilter: ["class", "data-theme"],
        });
        document.addEventListener("visibilitychange", sync);
        sync();

        cleanup = () => {
          nudge.current = () => undefined;
          if (raf) cancelAnimationFrame(raf);
          resizeObserver.disconnect();
          intersection.disconnect();
          theme.disconnect();
          document.removeEventListener("visibilitychange", sync);
          geometries.forEach((geometry) => geometry.dispose());
          materials.forEach((material) => material.dispose());
          grid.geometry.dispose();
          [grid.material].flat().forEach((material) => material.dispose());
          renderer.dispose();
          renderer.domElement.remove();
        };
      })
      .catch(() => {
        if (!disposed) onUnavailable();
      });

    return () => {
      disposed = true;
      cleanup();
    };
    // The loop is created once; later prop changes flow through `live`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      ref={host}
      className="lifecycle-canvas"
      data-testid="lifecycle-canvas"
    />
  );
}
