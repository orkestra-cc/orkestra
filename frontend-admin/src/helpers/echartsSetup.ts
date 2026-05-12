import * as echarts from 'echarts/core';
import { LegacyGridContainLabel } from 'echarts/features';

// ECharts 6 requires explicit registration for `grid.containLabel`, which
// the Orkestra dashboard charts rely on. Register once at startup so every
// chart's `echarts.use([...])` call inherits it instead of logging the
// "use `grid.outerBounds` instead" deprecation warning.
echarts.use([LegacyGridContainLabel]);
