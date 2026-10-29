import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      "/api": process.env.POLAR_API_URL || "http://192.168.1.200:8080",
    },
  },
});
