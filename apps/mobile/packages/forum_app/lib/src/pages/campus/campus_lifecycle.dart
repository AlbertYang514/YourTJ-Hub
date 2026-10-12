import 'package:flutter/widgets.dart';

/// campus 隐私内容的前台判定：仅 paused/hidden/detached 视为被覆盖——此时
/// 丢弃隐私子树、suspend 数据、冲刷内存缓存。inactive（通知栏、控制中心、
/// 来电等瞬时遮挡，内容仍在屏上）不触发重建，避免每次遮挡都整页骨架屏
/// 重载。应用切换器快照在 paused 之后由系统截取，此处收拢不削弱该防护。
bool isCampusForeground(AppLifecycleState? state) =>
    state == null ||
    state == AppLifecycleState.resumed ||
    state == AppLifecycleState.inactive;
