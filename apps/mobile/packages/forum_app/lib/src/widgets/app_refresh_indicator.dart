import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:ui_kit/ui_kit.dart';

import '../../l10n/app_localizations.dart';

/// Native refresh animation with a short, viewport-independent pull gesture.
/// [edgeOffset] keeps the indicator below overlaid navigation controls.
class AppRefreshIndicator extends StatefulWidget {
  const AppRefreshIndicator({
    super.key,
    required this.child,
    required this.onRefresh,
    this.edgeOffset = 0,
  });
  final Widget child;
  final RefreshCallback onRefresh;
  final double edgeOffset;

  @override
  State<AppRefreshIndicator> createState() => _AppRefreshIndicatorState();
}

class _AppRefreshIndicatorState extends State<AppRefreshIndicator>
    with SingleTickerProviderStateMixin {
  static const double _pullDistance = 72;
  static const double _displacement = 32;
  final _indicator = GlobalKey<RefreshIndicatorState>();
  late final AnimationController _rotation = AnimationController(
    vsync: this,
    duration: const Duration(seconds: 2),
  );
  bool _tracking = false;
  bool _requested = false;
  double _distance = 0;
  RefreshIndicatorStatus? _status;
  bool _updateScheduled = false;

  bool get _refreshing =>
      _requested ||
      _status == RefreshIndicatorStatus.snap ||
      _status == RefreshIndicatorStatus.refresh ||
      _status == RefreshIndicatorStatus.done;

  bool get _indicatorVisible =>
      _tracking ||
      _requested ||
      _status == RefreshIndicatorStatus.drag ||
      _status == RefreshIndicatorStatus.armed ||
      _status == RefreshIndicatorStatus.snap ||
      _status == RefreshIndicatorStatus.refresh ||
      _status == RefreshIndicatorStatus.done ||
      _status == RefreshIndicatorStatus.canceled;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _syncRotation();
  }

  void _onIndicatorStatusChange(RefreshIndicatorStatus? status) {
    if (!mounted) return;
    _status = status;
    _scheduleUpdate();
  }

  void _scheduleUpdate() {
    if (!mounted || _updateScheduled) return;
    _updateScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _updateScheduled = false;
      if (!mounted) return;
      _syncRotation();
      setState(() {});
    });
  }

  void _syncRotation() {
    final shouldRotate =
        (_requested || _status == RefreshIndicatorStatus.refresh) &&
        !MediaQuery.disableAnimationsOf(context);
    if (shouldRotate) {
      if (!_rotation.isAnimating) _rotation.repeat();
    } else if (_rotation.isAnimating) {
      _rotation.stop();
      _rotation.value = 0;
    }
  }

  void _clearDismissedIndicator() {
    if (!mounted ||
        _requested ||
        (_status != RefreshIndicatorStatus.canceled &&
            _status != RefreshIndicatorStatus.done)) {
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          _requested ||
          (_status != RefreshIndicatorStatus.canceled &&
              _status != RefreshIndicatorStatus.done)) {
        return;
      }
      setState(() => _status = null);
    });
  }

  void _release() {
    final refresh = _tracking && _distance >= _pullDistance;
    _tracking = false;
    _distance = 0;
    _scheduleUpdate();
    if (!refresh || _requested) return;
    final indicator = _indicator.currentState;
    if (indicator == null) return;
    _requested = true;
    _scheduleUpdate();
    // Keep Material's drag, trigger, completion and cancellation state machine;
    // noSpinner only gives this wrapper a place to draw the branded indicator.
    unawaited(
      indicator.show().whenComplete(() {
        _requested = false;
        _scheduleUpdate();
      }),
    );
  }

  bool _onScroll(ScrollNotification notification) {
    if (notification.depth != 0 ||
        notification.metrics.axisDirection != AxisDirection.down) {
      return false;
    }
    if (notification is ScrollStartNotification) {
      _tracking =
          notification.dragDetails != null &&
          notification.metrics.extentBefore == 0 &&
          !_requested;
      _distance = 0;
      _scheduleUpdate();
    } else if (_tracking) {
      if (notification.metrics.extentBefore > 0) {
        _tracking = false;
        _distance = 0;
        _scheduleUpdate();
      } else if (notification is ScrollEndNotification) {
        _release();
      } else {
        final details = switch (notification) {
          ScrollUpdateNotification() => notification.dragDetails,
          OverscrollNotification() => notification.dragDetails,
          _ => null,
        };
        if (details != null) {
          // Finger travel, not the rubber-band displacement: iOS damping must
          // not require a much longer pull than Android or a shorter viewport.
          final distance = math.max(0, _distance + details.delta.dy).toDouble();
          if (distance != _distance) {
            _distance = distance;
            _scheduleUpdate();
          }
        } else if (notification is ScrollUpdateNotification ||
            notification is OverscrollNotification) {
          // iOS begins its ballistic return before emitting ScrollEnd.
          _release();
        }
      }
    }
    return false;
  }

  @override
  Widget build(BuildContext context) {
    final reduceMotion = MediaQuery.disableAnimationsOf(context);
    final progress = (_distance / _pullDistance).clamp(0.0, 1.0).toDouble();
    final double top =
        widget.edgeOffset +
        (_refreshing ? _displacement : progress * _displacement);
    final hiding =
        _status == RefreshIndicatorStatus.canceled ||
        _status == RefreshIndicatorStatus.done;
    final localizations = AppLocalizations.of(context);
    final colors = Theme.of(context).colorScheme;

    return Stack(
      fit: StackFit.expand,
      clipBehavior: Clip.none,
      children: [
        RefreshIndicator.noSpinner(
          key: _indicator,
          onRefresh: widget.onRefresh,
          onStatusChange: _onIndicatorStatusChange,
          child: NotificationListener<ScrollNotification>(
            onNotification: _onScroll,
            child: widget.child,
          ),
        ),
        if (_indicatorVisible)
          Positioned(
            top: top,
            left: 0,
            right: 0,
            child: IgnorePointer(
              child: Align(
                alignment: Alignment.topCenter,
                child: AnimatedOpacity(
                  duration: reduceMotion
                      ? Duration.zero
                      : const Duration(milliseconds: 120),
                  opacity: hiding ? 0 : 1,
                  onEnd: _clearDismissedIndicator,
                  child: Semantics(
                    label: _refreshing
                        ? localizations.commonLoading
                        : MaterialLocalizations.of(
                            context,
                          ).refreshIndicatorSemanticLabel,
                    liveRegion: _refreshing,
                    child: Material(
                      key: const Key('app-refresh-indicator-pill'),
                      color: colors.surface,
                      elevation: 2,
                      shape: const StadiumBorder(),
                      child: Padding(
                        padding: EdgeInsets.symmetric(
                          horizontal: _refreshing ? 10 : 5,
                          vertical: 5,
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            RotationTransition(
                              key: const Key('app-refresh-indicator-rotation'),
                              turns: _rotation,
                              child: ExcludeSemantics(
                                child: GfLogo(
                                  key: const Key('app-refresh-indicator-logo'),
                                  size: _displacement,
                                  semanticLabel: null,
                                ),
                              ),
                            ),
                            if (_refreshing) ...[
                              const SizedBox(width: 6),
                              ExcludeSemantics(
                                child: Text(
                                  localizations.commonLoading,
                                  style: Theme.of(context).textTheme.labelSmall
                                      ?.copyWith(color: colors.onSurface),
                                ),
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }

  @override
  void dispose() {
    _rotation.dispose();
    super.dispose();
  }
}
