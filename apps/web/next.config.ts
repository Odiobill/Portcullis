import withRspack from "next-rspack";
import createNextIntlPlugin from 'next-intl/plugin';
import type { NextConfig } from "next";

const withNextIntl = createNextIntlPlugin();

const nextConfig: NextConfig = {
  output: 'standalone',
  /* config options here */
};

export default withNextIntl(withRspack(nextConfig));
