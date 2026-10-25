#!/bin/bash

set -e
set -o pipefail

# Ensure required arguments are set
if [[ -z "$input_video" ]] || [[ -z "$output_dir" ]]; then
    echo "Error: input_video and output_dir are required."
    exit 1
fi

# Ensure at least one of the hls_480p, hls_720p, or hls_1080p is set to 1
if [[ "$hls_480p" != "1" && "$hls_720p" != "1" && "$hls_1080p" != "1" ]]; then
    echo "Error: At least one of hls_480p, hls_720p, or hls_1080p must be set to 1."
    exit 1
fi

# Export input_video and output_dir to be accessible in the preset scripts
export input_video
export output_dir

# Create the output directory if it doesn't exist
mkdir -p "$output_dir"

complete_master_playlist() {
  height="$1"

  # Extract current bandwidth from the master playlist
  # Anchored on ":" so it can't match AVERAGE-BANDWIDTH
  current_bandwidth=$(sed -n 's/.*:BANDWIDTH=\([0-9]*\).*/\1/p' "$output_dir/master_${height}p.m3u8" | head -1)
  if [[ -z "$current_bandwidth" ]]; then
    echo "Failed to extract bandwidth"
    return 1
  fi

  # ffmpeg 8 writes a measured AVERAGE-BANDWIDTH; older versions get an estimate of 75% of peak
  attributes=",FRAME-RATE=30.000"
  if ! grep -q 'AVERAGE-BANDWIDTH=' "$output_dir/master_${height}p.m3u8"; then
    attributes=",AVERAGE-BANDWIDTH=$(($current_bandwidth * 75 / 100))$attributes"
  fi

  # Update master playlist with the new average bandwidth and frame rate
  if [[ "$(uname)" == "Darwin" ]]; then
    sed -i "" "s/\(:BANDWIDTH=$current_bandwidth\)/\1$attributes/" "$output_dir/master_${height}p.m3u8"
  else
    sed -i "s/\(:BANDWIDTH=$current_bandwidth\)/\1$attributes/" "$output_dir/master_${height}p.m3u8"
  fi

  if [ $? -ne 0 ]; then
    echo "Error updating master playlist"
    return 1
  fi
}

# Initialize the master playlist content
master_playlist_content="#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-INDEPENDENT-SEGMENTS"

# Process 480p if requested
if [[ "$hls_480p" == "1" ]]; then
  echo "Processing 480p..."
  bash "$(dirname "$0")/presets/hls_480p.sh"
  complete_master_playlist "480"
  master_playlist_content+="\n$(tail -n 3 "$output_dir/master_480p.m3u8")"
fi

# Process 720p if requested
if [[ "$hls_720p" == "1" ]]; then
  echo "Processing 720p..."
  bash "$(dirname "$0")/presets/hls_720p.sh"
  complete_master_playlist "720"
  master_playlist_content+="\n$(tail -n 3 "$output_dir/master_720p.m3u8")"
fi

# Process 1080p if requested
if [[ "$hls_1080p" == "1" ]]; then
  echo "Processing 1080p..."
  bash "$(dirname "$0")/presets/hls_1080p.sh"
  complete_master_playlist "1080"
  master_playlist_content+="\n$(tail -n 3 "$output_dir/master_1080p.m3u8")"
fi

# Write the combined master playlist to index.m3u8 in the output directory
echo -e "$master_playlist_content" > "$output_dir/index.m3u8"

echo "Master playlist created at $output_dir/index.m3u8"
