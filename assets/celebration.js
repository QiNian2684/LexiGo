(() => {
  "use strict";

  const motion = matchMedia("(prefers-reduced-motion: reduce)");
  const range = (start, end) => start + Math.random() * (end - start);
  let stopCurrent;

  window.WordReviewCelebrate = card => {
    stopCurrent?.();
    if (motion.matches || document.hidden || !card.getClientRects().length) return;

    const bounds = card.getBoundingClientRect();
    const width = document.documentElement.clientWidth;
    const height = document.documentElement.clientHeight;
    if (width <= 0 || height <= 0 || bounds.bottom <= 0 || bounds.top >= height) return;
    const canvas = document.createElement("canvas");
    const context = canvas.getContext("2d");
    if (!context) return;
    canvas.className = "completion-ink";
    canvas.setAttribute("aria-hidden", "true");
    // Limit the backing surface on both high-DPI phones and large displays.
    const scale = Math.min(devicePixelRatio || 1, 2, Math.sqrt(3000000 / (width * height)));
    canvas.width = Math.round(width * scale);
    canvas.height = Math.round(height * scale);
    context.scale(canvas.width / width, canvas.height / height);

    const android = document.body.classList.contains("platform-android");
    const cardWidth = Math.min(width, bounds.width);
    const desktopRoom = android ? 0 : Math.max(0, Math.min(1, (cardWidth - 480) / 330, (height - 440) / 210));
    const fullDesktop = desktopRoom >= .55;
    const count = (fullDesktop ? 7 : 5) + Math.floor(Math.random() * (android ? 2 : 3));
    const columns = Array.from({length: count}, (_, index) => index);
    const room = Math.max(.84, Math.min(1, cardWidth / (android ? 300 : 465), height / 470, bounds.height / 210));
    const spread = room * (android ? 1.12 : 1.24 + .25 * desktopRoom);
    const colors = document.documentElement.dataset.theme === "dark"
      ? ["#FFD45F", "#FFAA47", "#F6794C", "#ED6256"]
      : ["#E9A019", "#F48921", "#F36B3B", "#E65245"];
    const gravity = android ? 132 : 150 + 35 * desktopRoom;
    const interval = .52 / (count - 1);
    const weight = android ? 1 : 1 + .14 * desktopRoom;
    let delay = 0;
    const bursts = [];
    for (let index = 0; index < count; index++) {
      // Each burst occupies a different randomly selected column in the middle two thirds.
      const column = columns.splice(Math.floor(Math.random() * columns.length), 1)[0];
      const x = bounds.left + bounds.width * (1 / 6 + (column + range(.18, .82)) * 2 / (3 * count));
      const y = bounds.top + bounds.height * (column % 2 ? range(.18, .54) : range(.46, .82));
      const size = (fullDesktop ? 16 : 13) + Math.floor(Math.random() * 3);
      const rotation = range(0, Math.PI * 2);
      const particles = Array.from({length: size}, (_, i) => {
        const angle = rotation + (i + range(-.3, .3)) * Math.PI * 2 / size;
        const speed = range(105, 295) * spread;
        return {
          x: Math.cos(angle), y: Math.sin(angle), speed,
          lifetime: range(1.39, 1.72), weight: range(1.25, 2.85) * weight,
          trail: Math.random() < .42 ? range(2.3, 6.6) : 0,
          color: colors[Math.floor(Math.random() * colors.length)],
        };
      });
      bursts.push({x, y, delay, particles});
      delay += interval * range(.86, 1.06);
    }

    const start = performance.now();
    const end = Math.max(...bursts.map(({delay, particles}) =>
      delay + Math.max(...particles.map(particle => particle.lifetime))));
    let frame;
    let timeout;
    function stop() {
      cancelAnimationFrame(frame);
      clearTimeout(timeout);
      window.removeEventListener("resize", stop);
      document.removeEventListener("visibilitychange", stop);
      canvas.remove();
      if (stopCurrent === stop) stopCurrent = undefined;
    }
    stopCurrent = stop;
    window.addEventListener("resize", stop, {once: true});
    document.addEventListener("visibilitychange", stop, {once: true});
    document.body.append(canvas);

    function draw(timestamp) {
      const seconds = (timestamp - start) / 1000;
      if (seconds >= end || motion.matches || !card.getClientRects().length) {
        stop();
        return;
      }
      context.clearRect(0, 0, width, height);
      for (const burst of bursts) {
        const t = seconds - burst.delay;
        if (t < 0) continue;
        const distance = (1 - Math.exp(-2.15 * t)) / 2.15;
        const drop = .5 * gravity * t * t;
        for (const particle of burst.particles) {
          if (t >= particle.lifetime) continue;
          const travel = particle.speed * distance;
          const x = burst.x + particle.x * travel;
          const y = burst.y + particle.y * travel + drop;
          context.globalAlpha = .98 * (1 - (t / particle.lifetime) ** 2);
          context.beginPath();
          if (particle.trail) {
            const trail = particle.trail * (1 - t / particle.lifetime);
            context.moveTo(x - particle.x * trail, y - particle.y * trail);
            context.lineTo(x, y);
            context.lineCap = "round";
            context.lineWidth = particle.weight;
            context.strokeStyle = particle.color;
            context.stroke();
          } else {
            context.arc(x, y, particle.weight, 0, Math.PI * 2);
            context.fillStyle = particle.color;
            context.fill();
          }
        }
      }
      frame = requestAnimationFrame(draw);
    }
    frame = requestAnimationFrame(draw);
    timeout = setTimeout(stop, Math.ceil(end * 1000) + 150);
  };
})();
