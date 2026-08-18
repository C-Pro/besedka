export function initLoginBackground(canvas) {
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let width = 0;
    let height = 0;
    let dpr = 1;
    let animationFrameId = null;
    let isRunning = false;
    const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    const COLS = 54;
    const ROWS = 26;

    let gridU = [];
    let gridV = [];

    const ripples = [];
    let lastRippleTime = 0;

    function initGrid() {
        gridU = [];
        for (let c = 0; c < COLS; c++) {
            gridU.push((c / (COLS - 1)) * 2 - 1);
        }

        gridV = [];
        for (let r = 0; r < ROWS; r++) {
            gridV.push(r / (ROWS - 1));
        }
    }

    function resize() {
        dpr = Math.min(window.devicePixelRatio || 1, 2);
        width = window.innerWidth;
        height = window.innerHeight;

        canvas.width = Math.floor(width * dpr);
        canvas.height = Math.floor(height * dpr);
        canvas.style.width = `${width}px`;
        canvas.style.height = `${height}px`;

        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

        if (prefersReducedMotion) {
            renderFrame(0);
        }
    }

    function spawnRipple(time) {
        ripples.push({
            x: Math.random() * 2.4 - 1.2,
            z: Math.random() * 0.8 + 0.1,
            startTime: time,
            amplitude: 28 + Math.random() * 32,
            speed: 0.42 + Math.random() * 0.25,
            wavelength: 0.22 + Math.random() * 0.1,
            decay: 0.75 + Math.random() * 0.3
        });

        if (ripples.length > 8) {
            ripples.shift();
        }
    }

    function getDisplacement(x, z, time) {
        let dy = Math.sin(x * 3.5 + time * 1.2) * Math.cos(z * 3.2 - time * 0.9) * 9.0;
        dy += Math.sin(x * 1.8 - z * 2.4 + time * 0.7) * 7.0;

        for (let i = ripples.length - 1; i >= 0; i--) {
            const rip = ripples[i];
            const age = time - rip.startTime;
            if (age <= 0) continue;

            const radius = rip.speed * age;
            const dx = (x - rip.x) * 1.2;
            const dz = z - rip.z;
            const dist = Math.sqrt(dx * dx + dz * dz);
            const diff = dist - radius;

            if (Math.abs(diff) < rip.wavelength * 1.5) {
                const envelope = Math.exp(-rip.decay * age) * Math.cos((diff / (rip.wavelength * 1.5)) * (Math.PI / 2));
                if (envelope > 0.001) {
                    dy += Math.sin((diff / rip.wavelength) * Math.PI * 2) * rip.amplitude * envelope;
                }
            }

            if (age > 5.0) {
                ripples.splice(i, 1);
            }
        }

        return dy;
    }

    function project(colIdx, rowIdx, time) {
        const u = gridU[colIdx];
        const v = gridV[rowIdx];

        const horizonY = height * 0.32;
        const bottomY = height * 1.12;
        const totalSpan = bottomY - horizonY;

        const nearFactor = (1 - v) ** 1.7;
        const screenYBase = horizonY + totalSpan * nearFactor;

        const rowWidth = width * (1.35 + 1.45 * nearFactor);
        const depthScale = 0.22 + 0.78 * nearFactor;

        const worldX = u * 2.2;
        const worldZ = 1 - v;

        const dy = getDisplacement(worldX, worldZ, time);

        const screenX = width * 0.5 + u * (rowWidth * 0.5);
        const screenY = screenYBase - dy * depthScale * 1.8;

        return { x: screenX, y: screenY, depth: depthScale, nearFactor: nearFactor };
    }

    function renderFrame(timestamp) {
        const time = timestamp * 0.001;

        if (time - lastRippleTime > 1.6 + Math.random() * 1.2) {
            spawnRipple(time);
            lastRippleTime = time;
        }

        ctx.clearRect(0, 0, width, height);

        const pts = [];
        for (let r = 0; r < ROWS; r++) {
            pts[r] = [];
            for (let c = 0; c < COLS; c++) {
                pts[r][c] = project(c, r, time);
            }
        }

        for (let r = 0; r < ROWS; r++) {
            const near = pts[r][0].nearFactor;
            const alpha = Math.max(0.06, Math.min(0.55, 0.10 + 0.45 * near));
            ctx.strokeStyle = `rgba(59, 130, 246, ${alpha.toFixed(3)})`;
            ctx.lineWidth = Math.max(0.9, 1.4 * pts[r][0].depth);

            ctx.beginPath();
            ctx.moveTo(pts[r][0].x, pts[r][0].y);
            for (let c = 1; c < COLS; c++) {
                ctx.lineTo(pts[r][c].x, pts[r][c].y);
            }
            ctx.stroke();
        }

        for (let c = 0; c < COLS; c++) {
            ctx.beginPath();
            ctx.moveTo(pts[0][c].x, pts[0][c].y);

            for (let r = 1; r < ROWS; r++) {
                ctx.lineTo(pts[r][c].x, pts[r][c].y);
            }

            const pNear = pts[0][c];
            const pFar = pts[ROWS - 1][c];
            const grad = ctx.createLinearGradient(pFar.x, pFar.y, pNear.x, pNear.y);
            grad.addColorStop(0, 'rgba(59, 130, 246, 0.06)');
            grad.addColorStop(0.3, 'rgba(59, 130, 246, 0.25)');
            grad.addColorStop(1, 'rgba(96, 165, 250, 0.60)');

            ctx.strokeStyle = grad;
            ctx.lineWidth = 1.1;
            ctx.stroke();
        }

        if (isRunning && !prefersReducedMotion) {
            animationFrameId = requestAnimationFrame(renderFrame);
        }
    }

    function start() {
        if (!isRunning && !prefersReducedMotion) {
            isRunning = true;
            spawnRipple(0);
            spawnRipple(0.8);
            animationFrameId = requestAnimationFrame(renderFrame);
        }
    }

    function stop() {
        isRunning = false;
        if (animationFrameId) {
            cancelAnimationFrame(animationFrameId);
            animationFrameId = null;
        }
    }

    initGrid();
    resize();
    window.addEventListener('resize', resize, { passive: true });

    document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
            stop();
        } else {
            start();
        }
    });

    start();
}
