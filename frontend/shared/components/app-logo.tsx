"use client";

import Image from "next/image";

import { useBranding } from "@/shared/config/branding-provider";

const FALLBACK_LOGO_URL = "/logo.png";

type AppLogoProps = {
  alt?: string;
  width: number;
  height: number;
  priority?: boolean;
  className?: string;
};

export function AppLogo({
  alt,
  width,
  height,
  priority,
  className,
}: AppLogoProps) {
  const branding = useBranding();

  return (
    <Image
      src={branding.logoURL || FALLBACK_LOGO_URL}
      alt={alt ?? branding.title}
      width={width}
      height={height}
      priority={priority}
      className={className}
    />
  );
}

export function DeeixLogo({
  alt = "Ivano Chat",
  width,
  height,
  priority,
  className,
}: AppLogoProps) {
  const branding = useBranding();

  return (
    <Image
      src={branding.logoURL || FALLBACK_LOGO_URL}
      alt={alt}
      width={width}
      height={height}
      priority={priority}
      className={className}
    />
  );
}
