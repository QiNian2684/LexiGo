"use strict";

window.WordReviewChart = (() => {
  const surface = document.getElementById("chart-surface");
  const svg = document.getElementById("stats-chart");
  const legend = document.getElementById("chart-legend");
  const reading = document.getElementById("chart-reading");
  const switches = [...document.querySelectorAll("[data-chart-days], [data-chart-metric]")];
  const metrics = {
    words: [["distinct", "复习词数"], ["passed", "达标词数"]],
    answers: [["yes", "记住次数"], ["no", "没记住次数"]],
  };
  let history = [], data = [], range = 7, metric = "words", selected = -1;
  let bounds, highlight;

  function svgElement(tag, attributes, label) {
    const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value);
    if (label !== undefined) node.textContent = label;
    return node;
  }

  function select(index) {
    if (!data.length) return;
    const next = Math.max(0, Math.min(data.length - 1, index));
    const row = data[next];
    const detail = [row.day, ...metrics[metric].map(([key, label]) => `${label} ${row[key]}`)].join(" · ");
    if (reading.textContent !== detail) reading.textContent = detail;
    selected = next;
    highlight?.setAttribute("x", bounds.left + selected * bounds.slot);
  }

  function draw() {
    if (!history.length || !surface.clientWidth) return;
    data = history.slice(-range);
    const series = metrics[metric];
    const width = surface.clientWidth;
    const height = width < 490 ? 210 : 275;
    const top = 12, bottom = height - 32;
    const maximum = Math.max(1, ...data.flatMap(day => series.map(([key]) => day[key])));
    const magnitude = 10 ** Math.floor(Math.log10(Math.max(1, maximum / 4)));
    const step = [1, 2, 5, 10].find(n => n * magnitude * 4 >= maximum) * magnitude;
    const grid = Math.ceil(maximum / step);
    const left = String(grid * step).length * 7 + 11, right = width - 10;
    const slot = (right - left) / data.length;
    bounds = {left, slot};
    const x = index => left + (index + .5) * slot;
    const y = amount => bottom - amount * (bottom - top) / (grid * step);
    svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
    svg.replaceChildren();
    legend.dataset.metric = metric;
    legend.replaceChildren(...series.map(([, label], index) => {
      const node = document.createElement("span");
      node.className = `chart-label series-${index}`;
      node.textContent = label;
      return node;
    }));
    svg.dataset.metric = metric;
    for (let index = 0; index <= grid; index++) {
      const number = step * index, lineY = y(number);
      svg.append(svgElement("line", {x1: left, x2: right, y1: lineY, y2: lineY, class: "chart-grid"}),
        svgElement("text", {x: left - 6, y: lineY + 4, "text-anchor": "end"}, number));
    }
    highlight = svgElement("rect", {x: left, y: top, width: slot, height: bottom-top, rx: 5, class: "chart-highlight"});
    svg.append(highlight);
    for (let index = 0; index < data.length; index++) {
      const spacing = range === 30 ? (width < 490 ? 7 : 5) : (width < 325 ? 2 : 1);
      if (index === data.length - 1 || (index % spacing === 0 && index <= data.length - spacing)) {
        const label = data[index].day.slice(5).replace("-", "/");
        svg.append(svgElement("text", {x: x(index), y: bottom + 20, "text-anchor": "middle"}, label));
      }
    }
    series.forEach(([key], seriesIndex) => {
      const color = `series-${seriesIndex}`;
      if (range === 7) {
        const bar = Math.min(34, slot * .28);
        data.forEach((row, index) => {
          if (!row[key]) return;
          const rectX = x(index) + (seriesIndex ? 1 : -bar - 1), rectY = y(row[key]);
          svg.append(svgElement("rect", {x: rectX, y: rectY, width: bar, height: bottom - rectY,
            rx: Math.min(4, (bottom - rectY) / 2), class: `chart-mark ${color}`}));
        });
      } else {
        svg.append(svgElement("polyline", {points: data.map((row, index) => `${x(index)},${y(row[key])}`).join(" "),
          class: `chart-line ${color}`}));
        data.forEach((row, index) => svg.append(svgElement("circle", {cx: x(index), cy: y(row[key]),
          r: width < 490 ? 2 : 2.6, class: `chart-mark ${color}`})));
      }
    });
    select(selected < 0 ? data.length - 1 : selected);
  }

  for (const button of switches) button.addEventListener("click", () => {
    if (button.dataset.chartDays) {
      range = Number(button.dataset.chartDays);
      selected = -1;
    } else {
      metric = button.dataset.chartMetric;
    }
    for (const item of switches) {
      item.setAttribute("aria-pressed", String(item.dataset.chartDays ? Number(item.dataset.chartDays) === range : item.dataset.chartMetric === metric));
    }
    draw();
  });
  function selectAt(event) {
    if (!bounds) return;
    select(Math.floor((event.clientX - surface.getBoundingClientRect().left - bounds.left) / bounds.slot));
  }
  surface.addEventListener("pointermove", event => { if (event.pointerType === "mouse") selectAt(event); });
  surface.addEventListener("click", selectAt);
  surface.addEventListener("keydown", event => {
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      select(selected + (event.key === "ArrowLeft" ? -1 : 1));
    }
  });
  new ResizeObserver(draw).observe(surface);
  return {update(rows) {
    const previous = history.at(-1)?.day;
    history = rows;
    if (previous !== history.at(-1)?.day) selected = -1;
    draw();
  }};
})();
