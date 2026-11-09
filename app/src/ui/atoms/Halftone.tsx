"use client";
import { useEffect, useRef } from "react";
import { cn } from "@/lib/cn";
import { MARK_PATHS } from "./Avatar";
import { reduced } from "@/lib/motion";

/* The Atlas mark as a halftone: dots on a printed-drawing grid are sized by the logo, which ripples and drifts, with a light pass sweeping across it.
   One canvas, one fragment shader, palette read from the tokens. Still under reduced motion,
   paused when hidden or off-screen, capped at 30 fps. Without WebGL the CSS dot pattern below shows. */

const VERT = "attribute vec2 p;void main(){gl_Position=vec4(p,0.,1.);}";
const FRAG = `precision mediump float;
uniform vec2 res;uniform float t;uniform vec3 bg;uniform vec3 c1;uniform vec3 c2;uniform float cell;uniform sampler2D logo;
float h(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}
float n(vec2 p){vec2 i=floor(p),f=fract(p);f=f*f*(3.-2.*f);
  return mix(mix(h(i),h(i+vec2(1,0)),f.x),mix(h(i+vec2(0,1)),h(i+vec2(1,1)),f.x),f.y);}
float fbm(vec2 p){float v=0.,a=.5;for(int i=0;i<3;i++){v+=a*n(p);p*=2.02;a*=.5;}return v;}
void main(){
  vec2 cellId=floor(gl_FragCoord.xy/cell);
  vec2 c=(cellId+.5)*cell;                         // every dot is sized at its cell centre
  vec2 q=fract(gl_FragCoord.xy/cell)-.5;
  float size=min(res.x*.86,res.y*.62);
  vec2 lp=(c-res*vec2(.5,.52))/size+.5;
  // the mark breathes: a slow ripple runs through it and it drifts a little
  lp.x+=.016*sin(lp.y*8.+t*.9)+.012*sin(t*.35);
  lp.y+=.012*sin(lp.x*7.+t*.7)+.010*cos(t*.3);
  vec4 m=texture2D(logo,lp);
  float inside=step(.0,lp.x)*step(lp.x,1.)*step(.0,lp.y)*step(lp.y,1.);
  float shape=m.r*inside;
  // a bright pass sweeps across the mark every few seconds
  float sw=fract(t*.08)*2.6-.8;
  float band=smoothstep(.22,0.,abs((lp.x*.8+lp.y*.6)-sw));
  // quiet drifting field behind it
  vec2 uv=c/res.y;
  float f=fbm(uv*1.5+vec2(t*.035,-t*.02));
  f=smoothstep(.4,1.,f)*.34;
  float r=max(f,shape*(.5+.12*band))+band*.08*step(.1,f+shape);
  float a=1.-smoothstep(r-.06,r+.06,length(q));
  vec3 ink=mix(c2,c1,m.g*inside);                  // inner mark in teal, outer in ink
  vec3 col=shape>.2?ink:mix(c1,c2,.35);
  gl_FragColor=vec4(mix(bg,col,a*step(.03,r)),1.);
}`;

const hexToRgb = (v: string): [number, number, number] => {
  const s = v.trim().replace("#", "");
  const x = s.length === 3 ? s.split("").map((c) => c + c).join("") : s;
  return [0, 2, 4].map((i) => parseInt(x.slice(i, i + 2), 16) / 255) as [number, number, number];
};

export function Halftone({ className }: { className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const gl = canvas.getContext("webgl", { antialias: false, powerPreference: "low-power" });
    if (!gl || gl.isContextLost()) return; // the static dots stay visible
    const sh = (type: number, src: string) => { const s = gl.createShader(type)!; gl.shaderSource(s, src); gl.compileShader(s); return s; };
    const prog = gl.createProgram();
    gl.attachShader(prog, sh(gl.VERTEX_SHADER, VERT));
    gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, FRAG));
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) return;
    gl.useProgram(prog);
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(prog, "p");
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    const u = (name: string) => gl.getUniformLocation(prog, name);

    // The mark, drawn once into a texture: outer shape in red, inner shape in green.
    const lc = document.createElement("canvas"); lc.width = lc.height = 512;
    const lx = lc.getContext("2d")!;
    lx.scale(512 / 3024, 512 / 3000);
    lx.globalCompositeOperation = "lighter";
    lx.fillStyle = "#f00"; lx.fill(new Path2D(MARK_PATHS[0]));
    lx.fillStyle = "#0f0"; lx.fill(new Path2D(MARK_PATHS[1]));
    const tex = gl.createTexture();
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, lc);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.uniform1i(u("logo"), 0);

    const css = getComputedStyle(canvas);
    const col = (name: string, fallback: string) => hexToRgb(css.getPropertyValue(name) || fallback);
    gl.uniform3f(u("bg"), ...col("--ground", "#f2f4f7"));
    gl.uniform3f(u("c1"), ...col("--accent", "#00665f"));
    gl.uniform3f(u("c2"), ...col("--text", "#0e1210"));

    const reduce = reduced();
    let w = 0, h = 0, raf = 0, last = 0, visible = true;
    
    const resize = () => {
      const dpr = Math.min(devicePixelRatio || 1, 1.5);
      w = canvas.clientWidth; h = canvas.clientHeight;
      canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
      gl.viewport(0, 0, canvas.width, canvas.height);
      gl.uniform2f(u("res"), canvas.width, canvas.height);
      gl.uniform1f(u("cell"), 13 * dpr);
    };
    const draw = (now: number) => {
      gl.uniform1f(u("t"), now / 1000 + 20);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
    };
    const loop = (now: number) => {
      raf = requestAnimationFrame(loop);
      if (now - last < 33) return;
      last = now; draw(now);
    };
    const start = () => { if (!reduce && visible && !document.hidden && !raf) raf = requestAnimationFrame(loop); };
    const stop = () => { cancelAnimationFrame(raf); raf = 0; };

    resize(); draw(performance.now());
    const ro = new ResizeObserver(() => { resize(); draw(performance.now()); });
    ro.observe(canvas);
    const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; if (visible) start(); else stop(); });
    io.observe(canvas);
    const onVis = () => (document.hidden ? stop() : start());
    document.addEventListener("visibilitychange", onVis);
    // Lost context (GPU reset, too many contexts): hide the canvas so the static dots show, never the browser's sad face.
    const onLost = (e: Event) => { e.preventDefault(); stop(); canvas.dataset.live = "false"; };
    canvas.addEventListener("webglcontextlost", onLost);
    canvas.dataset.live = "true";
    start();
    return () => { stop(); ro.disconnect(); io.disconnect(); document.removeEventListener("visibilitychange", onVis); canvas.removeEventListener("webglcontextlost", onLost); };
  }, []);

  return (
    <div aria-hidden className={cn("pointer-events-none absolute inset-0 overflow-hidden bg-ground", className)}>
      {/* Fallback and first paint: a static dot grid, hidden once the shader is drawing. */}
      <svg className="absolute inset-0 size-full opacity-50" aria-hidden>
        <defs><pattern id="ht-dots" width="13" height="13" patternUnits="userSpaceOnUse"><circle cx="6.5" cy="6.5" r="1.5" fill="var(--accent)" /></pattern></defs>
        <rect width="100%" height="100%" fill="url(#ht-dots)" />
      </svg>
      <canvas ref={ref} className="absolute inset-0 size-full opacity-0 data-[live=false]:hidden data-[live=true]:animate-[halftone-in_600ms_var(--ease-out)_120ms_forwards]" />
    </div>
  );
}
