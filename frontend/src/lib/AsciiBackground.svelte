<script>
    // A live ASCII "swell" behind every page: a fine grid of characters whose
    // density rolls in slow, overlapping waves, quieter in the middle of the
    // screen where the content sits. Coloured with a slowly drifting blend of
    // Rosé Pine hues, read from the theme's --rp-* variables so it follows the
    // dark/light toggle.
    //
    // How it stays cheap:
    //  - each row is drawn as one string with fillText (cells are "glyph +
    //    space" in a monospace font), so a frame is one draw call per row,
    //    and the wave maths is split into per-row and per-column parts;
    //  - colour is a tiny low-resolution image (one pixel per few cells)
    //    stretched over the glyphs with 'source-in' compositing;
    //  - capped at FPS, paused while the tab is hidden, cells sized from the
    //    viewport (fewer on phones), device pixel ratio capped;
    //  - with prefers-reduced-motion, a single still frame.
    import { onMount } from 'svelte';

    let wrap;
    let canvas;

    // Density ramp, sparse to dense. Index 0 is blank.
    const RAMP = [' ', '.', '·', ':', '-', '=', '+', '*', '#', '%'];
    const HUES = ['--rp-iris', '--rp-foam', '--rp-pine', '--rp-rose', '--rp-gold', '--rp-love'];
    const FPS = 20; // the swell is slow; 20fps reads as smooth
    const FPS_SLOW = 12; // fallback when frames turn out expensive
    const COLOR_STEP = 4; // cells per colour pixel

    onMount(() => {
        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        const reduceMq = matchMedia('(prefers-reduced-motion: reduce)');
        const fineMq = matchMedia('(hover: hover) and (pointer: fine)');

        const colorCanvas = document.createElement('canvas');
        const colorCtx = colorCanvas.getContext('2d');

        let W = 0, H = 0, dpr = 1;
        let fs = 11; // font size, CSS px
        let adv = 6.6; // glyph advance; a cell is one glyph plus one space
        let cw = 13, ch = 17; // cell size, CSS px
        let cols = 0, rows = 0;
        let px, py, mask; // per-cell position (CSS px) and centre mask
        let line; // per-row character buffer
        let colA, colBs, colBc, colG; // per-column wave terms
        let colorImg = null;
        let palette = [];
        let raf = 0;
        let last = 0;
        let fps = FPS;
        let costMs = 0; // moving average of frame cost
        const t0 = performance.now();
        const stats = { frames: 0, ms: 0 };
        window.__asciiStats = stats;

        // Pointer "lantern": cells near the cursor glow a little
        const ptr = { x: -9999, y: -9999, sx: -9999, sy: -9999, on: 0, son: 0 };

        function hex(v) {
            v = v.trim().replace('#', '');
            if (v.length === 3) v = v.split('').map((c) => c + c).join('');
            const n = parseInt(v, 16);
            return Number.isFinite(n) ? [(n >> 16) & 255, (n >> 8) & 255, n & 255] : [196, 167, 231];
        }

        function readTheme() {
            const cs = getComputedStyle(document.documentElement);
            palette = HUES.map((name) => hex(cs.getPropertyValue(name) || '#c4a7e7'));
            // Light theme: the Dawn hues are pastel and wash out on the cream
            // page, so draw them about a third darker.
            if (document.documentElement.dataset.theme === 'light')
                palette = palette.map((rgb) => rgb.map((v) => Math.round(v * 0.65)));
        }

        const font = () => `500 ${fs}px ui-monospace, 'SF Mono', Menlo, Consolas, 'DejaVu Sans Mono', monospace`;

        function layout() {
            W = wrap.clientWidth || innerWidth;
            H = wrap.clientHeight || innerHeight;
            // Glyphs scale with the viewport: ~9px on phones, ~12px on big monitors
            fs = Math.min(12, Math.max(9, W / 165));
            // A faint background does not need full retina sharpness: cap the
            // ratio at 1.5 and the backing store at ~3M device pixels
            dpr = Math.min(window.devicePixelRatio || 1, 1.5);
            while (dpr > 1 && W * H * dpr * dpr > 3e6) dpr -= 0.25;

            ctx.font = font();
            adv = ctx.measureText('M').width || fs * 0.6;
            cw = adv * 2;
            ch = Math.round(fs * 1.5);
            cols = Math.ceil(W / cw) + 1;
            rows = Math.ceil(H / ch) + 1;

            canvas.width = Math.ceil(W * dpr);
            canvas.height = Math.ceil(H * dpr);
            canvas.style.width = W + 'px';
            canvas.style.height = H + 'px';

            const n = cols * rows;
            px = new Float32Array(n);
            py = new Float32Array(n);
            mask = new Float32Array(n);
            const cx = W / 2, cy = H * 0.5;
            const topBand = Math.min(260, Math.max(150, H * 0.2));
            for (let r = 0, i = 0; r < rows; r++) {
                for (let c = 0; c < cols; c++, i++) {
                    const x = c * cw, y = r * ch;
                    px[i] = x;
                    py[i] = y;
                    // Elliptical distance from the centre: quiet in the middle
                    const dx = (x - cx) / (W * 0.5), dy = (y - cy) / (H * 0.6);
                    const d = Math.min(1, Math.sqrt(dx * dx + dy * dy));
                    const s = Math.min(1, Math.max(0, (d - 0.1) / 0.8));
                    // ...and calmer behind the header band at the top
                    const q = Math.min(1, y / topBand);
                    mask[i] = (0.42 + 0.58 * s * s * (3 - 2 * s)) * (0.3 + 0.7 * q * q * (3 - 2 * q));
                }
            }
            // Each cell is "glyph + space", so a row string is 2*cols long
            colA = new Float64Array(cols);
            colBs = new Float64Array(cols);
            colBc = new Float64Array(cols);
            colG = new Float64Array(cols);
            line = new Array(cols * 2).fill(' ');

            colorCanvas.width = Math.ceil(cols / COLOR_STEP) + 2;
            colorCanvas.height = Math.ceil(rows / COLOR_STEP) + 2;
            colorImg = colorCtx.createImageData(colorCanvas.width, colorCanvas.height);
        }

        // The colour field: a slow diagonal drift through the palette, bent by
        // gentle waves so the bands are never straight.
        function paintColors(t) {
            const w = colorCanvas.width, h = colorCanvas.height;
            const d = colorImg.data;
            const n = palette.length;
            for (let y = 0, o = 0; y < h; y++) {
                const v = y / h;
                for (let x = 0; x < w; x++, o += 4) {
                    const u = x / w;
                    let p = u * 1.5 + v * 0.8 + t * 0.016 + 0.22 * Math.sin(u * 4.2 - t * 0.09) + 0.16 * Math.sin(v * 5.1 + t * 0.07);
                    p = (p - Math.floor(p)) * n;
                    const k = p | 0, f = p - k;
                    const a = palette[k % n], b = palette[(k + 1) % n];
                    const e = f * f * (3 - 2 * f);
                    d[o] = a[0] + (b[0] - a[0]) * e;
                    d[o + 1] = a[1] + (b[1] - a[1]) * e;
                    d[o + 2] = a[2] + (b[2] - a[2]) * e;
                    d[o + 3] = 255;
                }
            }
            colorCtx.putImageData(colorImg, 0, 0);
        }

        function frame(now) {
            const start = performance.now();
            const t = (now - t0) / 1000;
            const levels = RAMP.length - 1;
            const S = 1 / 190; // wave scale in CSS px, so waves look alike on any screen

            // Ease the lantern towards the pointer
            ptr.sx += (ptr.x - ptr.sx) * 0.2;
            ptr.sy += (ptr.y - ptr.sy) * 0.2;
            ptr.son += (ptr.on - ptr.son) * 0.08;
            const lr2 = 140 * 140;
            const lamp = ptr.son > 0.01;

            ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
            ctx.globalCompositeOperation = 'source-over';
            ctx.clearRect(0, 0, W, H);
            ctx.font = font();
            ctx.textBaseline = 'middle';
            ctx.fillStyle = '#fff';
            ctx.globalAlpha = 1;

            // Three rolling swells at different angles and speeds:
            //   a = sin(x + .32t + 1.6 sin(.8y - .21t))
            //   b = sin(.7425x - 1.2825y - .47t)
            //   g = sin(.735x + 1.26y + .26t + sin(.7x + .15t))
            // split into per-column and per-row parts, so each cell costs
            // two sines instead of five.
            for (let c = 0; c < cols; c++) {
                const x = c * cw * S;
                colA[c] = x + t * 0.32;
                const p = 0.7425 * x - t * 0.47;
                colBs[c] = Math.sin(p);
                colBc[c] = Math.cos(p);
                colG[c] = 0.735 * x + t * 0.26 + Math.sin(0.7 * x + t * 0.15);
            }

            for (let r = 0, i = 0; r < rows; r++) {
                let any = false;
                const y = r * ch * S;
                const rowA = 1.6 * Math.sin(0.8 * y - t * 0.21);
                const sq = Math.sin(1.2825 * y), cq = Math.cos(1.2825 * y);
                const rowG = 1.26 * y;
                for (let c = 0; c < cols; c++, i++) {
                    const a = Math.sin(colA[c] + rowA);
                    const b = colBs[c] * cq - colBc[c] * sq;
                    const g = Math.sin(colG[c] + rowG);
                    let v = (a * 0.45 + b * 0.33 + g * 0.27 + 1.05) / 2.1;
                    v = v * v * (3 - 2 * v); // contrast
                    v = v * mask[i] - 0.04;
                    if (lamp) {
                        const dx = px[i] - ptr.sx, dy = py[i] - ptr.sy;
                        const d2 = dx * dx + dy * dy;
                        if (d2 < lr2 * 4) v += 0.38 * ptr.son * Math.exp(-d2 / lr2);
                    }
                    let k = (v * (levels + 0.6)) | 0;
                    if (k > levels) k = levels;
                    if (k > 0) {
                        line[c * 2] = RAMP[k];
                        any = true;
                    } else line[c * 2] = ' ';
                }
                const ty = r * ch + ch / 2;
                if (any) ctx.fillText(line.join(''), 0, ty);
            }

            // Tint everything drawn with the colour field
            paintColors(t);
            ctx.globalCompositeOperation = 'source-in';
            ctx.imageSmoothingEnabled = true;
            const sw = colorCanvas.width - 1, sh = colorCanvas.height - 1;
            ctx.drawImage(colorCanvas, 0, 0, sw, sh, 0, 0, sw * COLOR_STEP * cw, sh * COLOR_STEP * ch);
            ctx.globalCompositeOperation = 'source-over';

            const cost = performance.now() - start;
            stats.frames++;
            stats.ms += cost;
            // Slow devices: if frames average over 12ms, drop to FPS_SLOW
            // until they are back under 8ms
            costMs = costMs ? costMs * 0.9 + cost * 0.1 : cost;
            if (stats.frames > 20) fps = costMs > 12 ? FPS_SLOW : costMs < 8 ? FPS : fps;
        }

        function loop(now) {
            raf = requestAnimationFrame(loop);
            if (now - last < 1000 / fps - 2) return;
            last = now;
            frame(now);
        }

        function start() {
            stop();
            if (reduceMq.matches || document.hidden) {
                frame(t0 + 20000); // one still frame
                return;
            }
            raf = requestAnimationFrame(loop);
        }

        function stop() {
            cancelAnimationFrame(raf);
            raf = 0;
        }

        let resizeTimer = 0;
        let lastW = 0;
        function onResize() {
            clearTimeout(resizeTimer);
            resizeTimer = setTimeout(() => {
                // Phones change the viewport height as the address bar slides;
                // only relayout for a real change.
                const w = wrap.clientWidth, h = wrap.clientHeight;
                if (w === lastW && Math.abs(h - H) < 120 && h <= rows * ch) return;
                lastW = w;
                layout();
                if (!raf) frame(performance.now());
            }, 150);
        }

        function onMove(e) {
            ptr.x = e.clientX;
            ptr.y = e.clientY;
            if (ptr.son < 0.01) {
                ptr.sx = ptr.x;
                ptr.sy = ptr.y;
            }
            ptr.on = 1;
        }
        const onLeave = () => (ptr.on = 0);

        const onVisibility = () => (document.hidden ? stop() : start());
        const observer = new MutationObserver(() => {
            readTheme();
            if (!raf) frame(performance.now());
        });
        observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

        readTheme();
        layout();
        lastW = W;
        start();
        // System fonts can settle after first paint; measure again once ready
        document.fonts?.ready?.then(() => {
            layout();
            if (!raf) frame(performance.now());
        });

        addEventListener('resize', onResize);
        document.addEventListener('visibilitychange', onVisibility);
        reduceMq.addEventListener?.('change', start);
        if (fineMq.matches) {
            addEventListener('pointermove', onMove, { passive: true });
            document.documentElement.addEventListener('pointerleave', onLeave);
        }

        return () => {
            stop();
            clearTimeout(resizeTimer);
            observer.disconnect();
            removeEventListener('resize', onResize);
            document.removeEventListener('visibilitychange', onVisibility);
            reduceMq.removeEventListener?.('change', start);
            removeEventListener('pointermove', onMove);
            document.documentElement.removeEventListener('pointerleave', onLeave);
            if (window.__asciiStats === stats) delete window.__asciiStats;
        };
    });
</script>

<div class="ascii-bg" aria-hidden="true" bind:this={wrap}>
    <canvas bind:this={canvas}></canvas>
</div>

<style>
    .ascii-bg {
        position: fixed;
        inset: 0;
        z-index: 0;
        overflow: hidden;
        pointer-events: none;
        opacity: var(--ascii-opacity, 0.4);
        transition: opacity var(--slow, 420ms) ease;
    }

    canvas {
        display: block;
    }
</style>
