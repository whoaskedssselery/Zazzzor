import { useEffect, useRef } from "react";
import { attachCamera, attachOwnCamera } from "@/features/voice-call";
import { cn } from "@/shared/ui";

/**
 * A camera filling its tile, cropped to the tile's shape as Discord does.
 * The user's own camera is a local preview, mirrored as in a mirror;
 * someone else's is received only while this is on screen, at the tile's size.
 */
export const CameraView = ({ userId, own }: { userId: string; own: boolean }) => {
  const video = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    return own ? attachOwnCamera(el) : attachCamera(userId, el);
  }, [userId, own]);
  return (
    // The video carries no sound: a voice is the microphone's, played with the call.
    <video
      ref={video}
      autoPlay
      muted
      playsInline
      aria-hidden
      className={cn("absolute inset-0 size-full bg-[#000] object-cover", own && "-scale-x-100")}
    />
  );
};
