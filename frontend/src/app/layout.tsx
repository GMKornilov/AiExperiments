import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL(process.env.SITE_URL ?? "http://localhost:3000"),
  title: "Тихий помол — AI-бариста",
  description: "Персональный AI-бариста для настройки кофе и точных рецептов.",
  openGraph: {
    title: "Тихий помол — AI-бариста",
    description: "Персональный AI-бариста для настройки кофе и точных рецептов.",
    locale: "ru_RU",
    type: "website",
    images: [
      {
        url: "/barista-social.jpg",
        width: 1200,
        height: 630,
        alt: "Чашка эспрессо в тёплом свете",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Тихий помол — AI-бариста",
    description: "Персональный AI-бариста для настройки кофе и точных рецептов.",
    images: ["/barista-social.jpg"],
  },
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="ru">
      <body>{children}</body>
    </html>
  );
}
