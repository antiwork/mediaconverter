#!/bin/bash

set -ex

SCALE_FILTER=${SCALE_FILTER:-scale}

if [ "$SCALE_FILTER" = "zscale" ]; then
    FILTER_OPTS="zscale=-2:1080:min=bt709:m=bt709"
else
    FILTER_OPTS="scale=-2:1080"
fi

ffmpeg -i "$input_video" \
-vf "$FILTER_OPTS" \
-c:v libx264 \
-b:v 4000k \
-maxrate 4000k \
-bufsize 4000k \
-pix_fmt yuv420p \
-g 60 \
-keyint_min 60 \
-sc_threshold 0 \
-refs 3 \
-profile:v main \
-x264-params "aq-mode=2:qpmin=0:qpmax=51:me=umh:subme=7:bframes=0" \
-framerate 30 \
-r 30 \
-c:a aac \
-b:a 128k \
-ac 2 \
-ar 44100 \
-f hls \
-hls_time 9 \
-hls_list_size 0 \
-hls_segment_type mpegts \
-hls_flags delete_segments \
-master_pl_name master_1080p.m3u8 \
-hls_segment_filename "$output_dir/index_1080p_%05d.ts" \
"$output_dir/index_1080p.m3u8"
