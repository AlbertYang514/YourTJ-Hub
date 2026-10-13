import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'twemoji_asset_map.g.dart';

/// A text segment that should use the bundled Twemoji artwork.
@immutable
class TwemojiSegment {
  const TwemojiSegment.text(this.value) : asset = null;
  const TwemojiSegment.emoji(this.value, this.asset);

  final String value;
  final String? asset;

  bool get isEmoji => asset != null;

  @override
  bool operator ==(Object other) =>
      other is TwemojiSegment && other.value == value && other.asset == asset;

  @override
  int get hashCode => Object.hash(value, asset);
}

const String twemojiVersion = twemojiAssetVersion;

class _TwemojiSelectionRegistry {
  final Map<String, int> _placeholderByEmoji = <String, int>{};
  final Map<int, String> emojiByPlaceholder = <int, String>{};
  final Set<int> _sourceCodeUnits = <int>{};
  var _nextPlaceholder = 0xe000;

  void reserveSource(String source) {
    _sourceCodeUnits.addAll(source.codeUnits);
  }

  int placeholderFor(String emoji) =>
      _placeholderByEmoji.putIfAbsent(emoji, () {
        while (_nextPlaceholder <= 0xf8ff &&
            (_sourceCodeUnits.contains(_nextPlaceholder) ||
                emojiByPlaceholder.containsKey(_nextPlaceholder))) {
          _nextPlaceholder++;
        }
        if (_nextPlaceholder > 0xf8ff) {
          return PlaceholderSpan.placeholderCodeUnit;
        }
        final placeholder = _nextPlaceholder++;
        emojiByPlaceholder[placeholder] = emoji;
        return placeholder;
      });

  String restoreSelectedText(String text) => String.fromCharCodes(
    text.runes.expand((codepoint) {
      final emoji = emojiByPlaceholder[codepoint];
      return emoji == null ? <int>[codepoint] : emoji.runes;
    }),
  );
}

InlineSpan buildTwemojiTextSpan(
  BuildContext context,
  InlineSpan sourceSpan, {
  required TextStyle inheritedStyle,
  required TextScaler textScaler,
}) {
  if (sourceSpan is TextSpan &&
      !parseTwemojiText(
        sourceSpan.toPlainText(includePlaceholders: false),
      ).any((segment) => segment.isEmoji)) {
    return sourceSpan;
  }
  final registry = _TwemojiSelectionScope.maybeOf(context);
  registry?.reserveSource(sourceSpan.toPlainText());
  return _TwemojiSpanBuilder(
    textScaler,
    registry,
  ).build(sourceSpan, inheritedStyle);
}

class _TwemojiSelectionScope extends InheritedWidget {
  const _TwemojiSelectionScope({required this.registry, required super.child});

  final _TwemojiSelectionRegistry registry;

  static _TwemojiSelectionRegistry? maybeOf(BuildContext context) => context
      .dependOnInheritedWidgetOfExactType<_TwemojiSelectionScope>()
      ?.registry;

  @override
  bool updateShouldNotify(_TwemojiSelectionScope oldWidget) =>
      !identical(registry, oldWidget.registry);
}

/// A platform-adaptive selection area that restores inline image placeholders
/// to their original Unicode Emoji sequences when selected text is copied.
class GfSelectionArea extends StatefulWidget {
  const GfSelectionArea({
    super.key,
    required this.child,
    this.focusNode,
    this.selectionControls,
    this.magnifierConfiguration,
    this.contextMenuBuilder,
  });

  final Widget child;
  final FocusNode? focusNode;
  final TextSelectionControls? selectionControls;
  final TextMagnifierConfiguration? magnifierConfiguration;
  final GfSelectionAreaContextMenuBuilder? contextMenuBuilder;

  @override
  State<GfSelectionArea> createState() => _GfSelectionAreaState();
}

typedef GfSelectionAreaContextMenuBuilder =
    Widget Function(
      BuildContext context,
      SelectableRegionState selection,
      List<ContextMenuButtonItem> buttonItems,
    );

class _GfSelectionAreaState extends State<GfSelectionArea> {
  final _TwemojiSelectionRegistry _registry = _TwemojiSelectionRegistry();
  String? _selectedText;

  List<ContextMenuButtonItem> _buttonItems(SelectableRegionState selection) =>
      selection.contextMenuButtonItems
          .map((item) {
            if (item.type != ContextMenuButtonType.copy) return item;
            return ContextMenuButtonItem(
              type: ContextMenuButtonType.copy,
              onPressed: () {
                _copySelectedText();
                _finishCopy(selection);
              },
            );
          })
          .toList(growable: false);

  void _copySelectedText() {
    final selectedText = _selectedText;
    if (selectedText == null) return;
    Clipboard.setData(
      ClipboardData(text: _registry.restoreSelectedText(selectedText)),
    );
  }

  void _finishCopy(SelectableRegionState selection) {
    switch (defaultTargetPlatform) {
      case TargetPlatform.android:
      case TargetPlatform.fuchsia:
        selection.clearSelection();
      case TargetPlatform.iOS:
        selection.hideToolbar(false);
      case TargetPlatform.linux:
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        selection.hideToolbar();
    }
  }

  @override
  Widget build(BuildContext context) => SelectionArea(
    focusNode: widget.focusNode,
    selectionControls: widget.selectionControls,
    magnifierConfiguration: widget.magnifierConfiguration,
    onSelectionChanged: (content) => _selectedText = content?.plainText,
    contextMenuBuilder: (context, selection) {
      final buttonItems = _buttonItems(selection);
      return widget.contextMenuBuilder?.call(context, selection, buttonItems) ??
          AdaptiveTextSelectionToolbar.buttonItems(
            anchors: selection.contextMenuAnchors,
            buttonItems: buttonItems,
          );
    },
    child: _TwemojiSelectionScope(
      registry: _registry,
      child: Actions(
        actions: <Type, Action<Intent>>{
          CopySelectionTextIntent: CallbackAction<CopySelectionTextIntent>(
            onInvoke: (_) {
              _copySelectedText();
              return null;
            },
          ),
        },
        child: widget.child,
      ),
    ),
  );
}

/// Matches the longest complete sequence in the pinned Twemoji 17.0.3 map.
/// The returned strings always concatenate to [text] unchanged.
List<TwemojiSegment> parseTwemojiText(String text) {
  final codepoints = text.runes.toList(growable: false);
  final result = <TwemojiSegment>[];
  final plain = StringBuffer();
  var index = 0;

  void flushPlain() {
    if (plain.isEmpty) return;
    result.add(TwemojiSegment.text(plain.toString()));
    plain.clear();
  }

  while (index < codepoints.length) {
    var candidate = '';
    var hasJoiner = false;
    var bestEnd = -1;
    String? bestAsset;
    final limit = (index + twemojiMaximumCodepoints)
        .clamp(0, codepoints.length)
        .toInt();

    for (var end = index; end < limit; end++) {
      candidate += String.fromCharCode(codepoints[end]);
      hasJoiner = hasJoiner || codepoints[end] == 0x200d;
      final key = hasJoiner ? candidate : candidate.replaceAll('\uFE0F', '');
      final asset = twemojiAssetBySequence[key];
      if (asset != null) {
        bestEnd = end + 1;
        bestAsset = asset;
      }
    }

    // If this is an unsupported ZWJ or modifier sequence, leave the complete
    // cluster as text instead of painting only its first recognized emoji.
    if (bestAsset != null && bestEnd > index) {
      final joinedEnd = _consumeEmojiExtensions(codepoints, bestEnd);
      if (joinedEnd < codepoints.length && codepoints[joinedEnd] == 0x200d) {
        final unsupportedEnd = _consumeJoinedSequence(codepoints, index);
        for (var cursor = index; cursor < unsupportedEnd; cursor++) {
          plain.writeCharCode(codepoints[cursor]);
        }
        index = unsupportedEnd;
        continue;
      }
      if (joinedEnd > bestEnd) {
        final extended = String.fromCharCodes(
          codepoints.sublist(index, joinedEnd),
        );
        final hasJoinerInExtended = extended.contains('\u200D');
        final extendedKey = hasJoinerInExtended
            ? extended
            : extended.replaceAll('\uFE0F', '');
        final extendedAsset = twemojiAssetBySequence[extendedKey];
        if (extendedAsset == null) {
          for (var cursor = index; cursor < joinedEnd; cursor++) {
            plain.writeCharCode(codepoints[cursor]);
          }
          index = joinedEnd;
          continue;
        }
        bestEnd = joinedEnd;
        bestAsset = extendedAsset;
      }

      flushPlain();
      result.add(
        TwemojiSegment.emoji(
          String.fromCharCodes(codepoints.sublist(index, bestEnd)),
          bestAsset,
        ),
      );
      index = bestEnd;
      continue;
    }

    final joinedEnd = _consumeJoinedSequence(codepoints, index);
    if (joinedEnd > index + 1) {
      for (var cursor = index; cursor < joinedEnd; cursor++) {
        plain.writeCharCode(codepoints[cursor]);
      }
      index = joinedEnd;
      continue;
    }

    plain.writeCharCode(codepoints[index]);
    index++;
  }

  flushPlain();
  return result;
}

int _consumeEmojiExtensions(List<int> codepoints, int start) {
  var end = start;
  while (end < codepoints.length && _isEmojiExtension(codepoints[end])) {
    end++;
  }
  return end;
}

int _consumeJoinedSequence(List<int> codepoints, int start) {
  var end = _consumeEmojiExtensions(codepoints, start + 1);
  var hasJoiner = false;
  while (end < codepoints.length && codepoints[end] == 0x200d) {
    hasJoiner = true;
    end++;
    if (end < codepoints.length) end++;
    end = _consumeEmojiExtensions(codepoints, end);
  }
  return hasJoiner ? end : start + 1;
}

bool _isEmojiExtension(int codepoint) =>
    codepoint == 0xfe0e ||
    codepoint == 0xfe0f ||
    codepoint == 0x20e3 ||
    (codepoint >= 0x1f3fb && codepoint <= 0x1f3ff) ||
    (codepoint >= 0xe0020 && codepoint <= 0xe007f);

/// Inline text renderer shared by all mobile app surfaces.
///
/// Input fields deliberately remain Flutter's native text widgets so IME and
/// cursor behavior are untouched. Display text keeps its original Unicode in
/// the span tree; image spans expose the same sequence as semantic text.
class GfText extends Text {
  const GfText(
    super.data, {
    super.key,
    super.style,
    super.strutStyle,
    super.textAlign,
    super.textDirection,
    super.locale,
    super.softWrap,
    super.overflow,
    super.textScaler,
    super.maxLines,
    super.semanticsLabel,
    super.textWidthBasis,
    super.textHeightBehavior,
    super.selectionColor,
  });

  const GfText.rich(
    super.textSpan, {
    super.key,
    super.style,
    super.strutStyle,
    super.textAlign,
    super.textDirection,
    super.locale,
    super.softWrap,
    super.overflow,
    super.textScaler,
    super.maxLines,
    super.semanticsLabel,
    super.textWidthBasis,
    super.textHeightBehavior,
    super.selectionColor,
  }) : super.rich();

  @override
  Widget build(BuildContext context) {
    final defaultTextStyle = DefaultTextStyle.of(context);
    var effectiveStyle = style;
    if (effectiveStyle == null || effectiveStyle.inherit) {
      effectiveStyle = defaultTextStyle.style.merge(effectiveStyle);
    }
    if (MediaQuery.boldTextOf(context)) {
      effectiveStyle = effectiveStyle.merge(
        const TextStyle(fontWeight: FontWeight.bold),
      );
    }
    final scaler = textScaler ?? MediaQuery.textScalerOf(context);
    final sourceSpan = TextSpan(
      text: data,
      style: effectiveStyle,
      locale: locale,
      children: textSpan == null ? null : <InlineSpan>[textSpan!],
    );
    final rendered =
        buildTwemojiTextSpan(
              context,
              sourceSpan,
              inheritedStyle: effectiveStyle,
              textScaler: scaler,
            )
            as TextSpan;
    final lineHeightScaleFactor =
        MediaQuery.maybeLineHeightScaleFactorOverrideOf(context);
    final letterSpacing = MediaQuery.maybeLetterSpacingOverrideOf(context);
    final wordSpacing = MediaQuery.maybeWordSpacingOverrideOf(context);
    final overrideStyle = TextStyle(
      height: lineHeightScaleFactor,
      letterSpacing: letterSpacing,
      wordSpacing: wordSpacing,
    );
    final effectiveSpan = _applyTextStyleOverrides(rendered, overrideStyle);
    final effectiveStrutStyle = strutStyle?.merge(
      StrutStyle(height: lineHeightScaleFactor),
    );
    final selectionStyle = DefaultSelectionStyle.of(context);
    Widget result = RichText(
      text: effectiveSpan,
      textAlign: textAlign ?? defaultTextStyle.textAlign ?? TextAlign.start,
      textDirection: textDirection,
      locale: locale,
      softWrap: softWrap ?? defaultTextStyle.softWrap,
      overflow:
          overflow ?? effectiveStyle.overflow ?? defaultTextStyle.overflow,
      textScaler: scaler,
      maxLines: maxLines ?? defaultTextStyle.maxLines,
      strutStyle: effectiveStrutStyle,
      textWidthBasis: textWidthBasis ?? defaultTextStyle.textWidthBasis,
      textHeightBehavior:
          textHeightBehavior ??
          defaultTextStyle.textHeightBehavior ??
          DefaultTextHeightBehavior.maybeOf(context),
      selectionRegistrar: SelectionContainer.maybeOf(context),
      selectionColor:
          selectionColor ??
          selectionStyle.selectionColor ??
          DefaultSelectionStyle.defaultColor,
    );
    if (SelectionContainer.maybeOf(context) != null) {
      result = MouseRegion(
        cursor: selectionStyle.mouseCursor ?? SystemMouseCursors.text,
        child: result,
      );
    }
    if (semanticsLabel != null || semanticsIdentifier != null) {
      result = Semantics(
        textDirection: textDirection,
        label: semanticsLabel,
        identifier: semanticsIdentifier,
        child: ExcludeSemantics(
          excluding: semanticsLabel != null,
          child: result,
        ),
      );
    }
    return result;
  }
}

TextSpan _applyTextStyleOverrides(TextSpan span, TextStyle overrideStyle) {
  final children = span.children?.map((child) {
    if (child is TextSpan && child.runtimeType == TextSpan) {
      return _applyTextStyleOverrides(child, overrideStyle);
    }
    return child;
  }).toList();
  return TextSpan(
    text: span.text,
    children: children,
    style: span.style?.merge(overrideStyle) ?? overrideStyle,
    recognizer: span.recognizer,
    mouseCursor: span.mouseCursor,
    onEnter: span.onEnter,
    onExit: span.onExit,
    semanticsLabel: span.semanticsLabel,
    semanticsIdentifier: span.semanticsIdentifier,
    locale: span.locale,
    spellOut: span.spellOut,
  );
}

/// Selectable text maps a selected image placeholder back to its full Unicode
/// sequence when the platform Copy action is used.
class GfSelectableText extends StatelessWidget {
  const GfSelectableText(
    this.data, {
    super.key,
    this.focusNode,
    this.style,
    this.strutStyle,
    this.textAlign,
    this.textDirection,
    this.textScaler,
    this.showCursor = false,
    this.autofocus = false,
    this.minLines,
    this.maxLines,
    this.cursorWidth = 2,
    this.cursorHeight,
    this.cursorRadius,
    this.cursorColor,
    this.selectionColor,
    this.dragStartBehavior = DragStartBehavior.start,
    this.enableInteractiveSelection = true,
    this.selectionControls,
    this.onTap,
    this.scrollPhysics,
    this.scrollBehavior,
    this.semanticsLabel,
    this.textHeightBehavior,
    this.textWidthBasis,
    this.magnifierConfiguration,
    this.contextMenuBuilder,
  }) : textSpan = null;

  const GfSelectableText.rich(
    this.textSpan, {
    super.key,
    this.focusNode,
    this.style,
    this.strutStyle,
    this.textAlign,
    this.textDirection,
    this.textScaler,
    this.showCursor = false,
    this.autofocus = false,
    this.minLines,
    this.maxLines,
    this.cursorWidth = 2,
    this.cursorHeight,
    this.cursorRadius,
    this.cursorColor,
    this.selectionColor,
    this.dragStartBehavior = DragStartBehavior.start,
    this.enableInteractiveSelection = true,
    this.selectionControls,
    this.onTap,
    this.scrollPhysics,
    this.scrollBehavior,
    this.semanticsLabel,
    this.textHeightBehavior,
    this.textWidthBasis,
    this.magnifierConfiguration,
    this.contextMenuBuilder,
  }) : data = null;

  final String? data;
  final InlineSpan? textSpan;
  final FocusNode? focusNode;
  final TextStyle? style;
  final StrutStyle? strutStyle;
  final TextAlign? textAlign;
  final TextDirection? textDirection;
  final TextScaler? textScaler;
  final bool showCursor;
  final bool autofocus;
  final int? minLines;
  final int? maxLines;
  final double cursorWidth;
  final double? cursorHeight;
  final Radius? cursorRadius;
  final Color? cursorColor;
  final Color? selectionColor;
  final DragStartBehavior dragStartBehavior;
  final bool enableInteractiveSelection;
  final TextSelectionControls? selectionControls;
  final GestureTapCallback? onTap;
  final ScrollPhysics? scrollPhysics;
  final ScrollBehavior? scrollBehavior;
  final String? semanticsLabel;
  final TextHeightBehavior? textHeightBehavior;
  final TextWidthBasis? textWidthBasis;
  final TextMagnifierConfiguration? magnifierConfiguration;
  final EditableTextContextMenuBuilder? contextMenuBuilder;

  @override
  Widget build(BuildContext context) {
    final inheritedStyle = DefaultTextStyle.of(context).style;
    final baseStyle = inheritedStyle.merge(style);
    final scaler = textScaler ?? MediaQuery.textScalerOf(context);
    final sourceSpan = textSpan ?? TextSpan(text: data ?? '');
    final containsEmoji = parseTwemojiText(
      sourceSpan.toPlainText(includePlaceholders: false),
    ).any((segment) => segment.isEmoji);

    if (containsEmoji) {
      final richText = GfText.rich(
        sourceSpan,
        style: style,
        strutStyle: strutStyle,
        textAlign: textAlign,
        textDirection: textDirection,
        textScaler: textScaler,
        maxLines: maxLines,
        semanticsLabel: semanticsLabel,
        textWidthBasis: textWidthBasis,
        textHeightBehavior: textHeightBehavior,
        selectionColor: selectionColor,
      );
      if (!enableInteractiveSelection) {
        return SelectionContainer.disabled(child: richText);
      }
      if (SelectionContainer.maybeOf(context) != null) return richText;
      return GfSelectionArea(
        focusNode: focusNode,
        selectionControls: selectionControls,
        magnifierConfiguration: magnifierConfiguration,
        child: richText,
      );
    }

    final builder = _TwemojiSpanBuilder(scaler);
    final rendered = builder.build(sourceSpan, baseStyle) as TextSpan;

    return SelectableText.rich(
      rendered,
      focusNode: focusNode,
      style: style,
      strutStyle: strutStyle,
      textAlign: textAlign,
      textDirection: textDirection,
      textScaler: textScaler,
      showCursor: showCursor,
      autofocus: autofocus,
      minLines: minLines,
      maxLines: maxLines,
      cursorWidth: cursorWidth,
      cursorHeight: cursorHeight,
      cursorRadius: cursorRadius,
      cursorColor: cursorColor,
      selectionColor: selectionColor,
      dragStartBehavior: dragStartBehavior,
      enableInteractiveSelection: enableInteractiveSelection,
      selectionControls: selectionControls,
      onTap: onTap,
      scrollPhysics: scrollPhysics,
      scrollBehavior: scrollBehavior,
      semanticsLabel: semanticsLabel,
      textHeightBehavior: textHeightBehavior,
      textWidthBasis: textWidthBasis,
      magnifierConfiguration: magnifierConfiguration,
      contextMenuBuilder:
          contextMenuBuilder ??
          (BuildContext context, EditableTextState state) {
            final items = state.contextMenuButtonItems
                .map((item) {
                  if (item.type != ContextMenuButtonType.copy) return item;
                  return ContextMenuButtonItem(
                    type: ContextMenuButtonType.copy,
                    onPressed: () {
                      final selection = state.textEditingValue.selection;
                      if (selection.isValid && !selection.isCollapsed) {
                        final sourceRange = builder.sourceRangeForDisplayRange(
                          selection.start,
                          selection.end,
                        );
                        final sourceText = sourceSpan.toPlainText(
                          includePlaceholders: true,
                        );
                        Clipboard.setData(
                          ClipboardData(
                            text: sourceText.substring(
                              sourceRange.start
                                  .clamp(0, sourceText.length)
                                  .toInt(),
                              sourceRange.end
                                  .clamp(0, sourceText.length)
                                  .toInt(),
                            ),
                          ),
                        );
                      }
                      state.hideToolbar();
                    },
                  );
                })
                .toList(growable: false);
            return AdaptiveTextSelectionToolbar.buttonItems(
              anchors: state.contextMenuAnchors,
              buttonItems: items,
            );
          },
    );
  }
}

class _TwemojiSpanBuilder {
  _TwemojiSpanBuilder(this.textScaler, [this.selectionRegistry]);

  final TextScaler textScaler;
  final _TwemojiSelectionRegistry? selectionRegistry;
  final List<int> _displayToSource = <int>[0];
  var _sourceOffset = 0;

  InlineSpan build(InlineSpan span, TextStyle inheritedStyle) {
    if (span is TextSpan &&
        !parseTwemojiText(
          span.toPlainText(includePlaceholders: false),
        ).any((segment) => segment.isEmoji)) {
      return span;
    }
    return _transform(span, inheritedStyle);
  }

  TextSelection sourceRangeForDisplayRange(int start, int end) {
    final safeStart = start.clamp(0, _displayToSource.length - 1).toInt();
    final safeEnd = end.clamp(0, _displayToSource.length - 1).toInt();
    return TextSelection(
      baseOffset: _displayToSource[safeStart],
      extentOffset: _displayToSource[safeEnd],
    );
  }

  InlineSpan _transform(InlineSpan span, TextStyle inheritedStyle) {
    if (span is WidgetSpan) {
      _sourceOffset++;
      _displayToSource.add(_sourceOffset);
      return span;
    }
    if (span is! TextSpan) return span;

    final style = inheritedStyle.merge(span.style);
    final transformedChildren = <InlineSpan>[];
    if (span.text case final text?) {
      final segments = parseTwemojiText(text);
      for (final segment in segments) {
        if (!segment.isEmoji) {
          final value = segment.value;
          transformedChildren.add(
            TextSpan(
              text: value,
              recognizer: span.recognizer,
              mouseCursor: span.mouseCursor,
              onEnter: span.onEnter,
              onExit: span.onExit,
            ),
          );
          _sourceOffset += value.length;
          for (var i = 0; i < value.length; i++) {
            _displayToSource.add(_sourceOffset - value.length + i + 1);
          }
          continue;
        }

        final value = segment.value;
        final sourceStart = _sourceOffset;
        _sourceOffset += value.length;
        _displayToSource.add(_sourceOffset);
        final fontSize = style.fontSize ?? 14;
        final dimension = textScaler
            .scale(fontSize)
            .clamp(1.0, 512.0)
            .toDouble();
        final tapRecognizer = span.recognizer;
        final VoidCallback? onTap = tapRecognizer is TapGestureRecognizer
            ? tapRecognizer.onTap
            : null;
        Widget emoji = Semantics(
          label: value,
          image: true,
          container: true,
          excludeSemantics: true,
          onTap: onTap,
          child: Image.asset(
            'assets/twemoji-public/twemoji/72x72/${segment.asset}',
            package: 'ui_kit',
            width: dimension,
            height: dimension,
            cacheWidth: 72,
            cacheHeight: 72,
            filterQuality: FilterQuality.medium,
            errorBuilder: (context, error, stack) =>
                Text(value, style: TextStyle(fontSize: dimension, height: 1)),
          ),
        );
        if (onTap != null) {
          emoji = GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: onTap,
            child: emoji,
          );
        }
        transformedChildren.add(
          _TwemojiWidgetSpan(
            placeholder:
                selectionRegistry?.placeholderFor(value) ??
                PlaceholderSpan.placeholderCodeUnit,
            child: emoji,
          ),
        );
        assert(_sourceOffset > sourceStart);
      }
    }

    for (final child in span.children ?? const <InlineSpan>[]) {
      transformedChildren.add(_transform(child, style));
    }

    return TextSpan(
      style: span.style,
      children: transformedChildren,
      recognizer: span.recognizer,
      mouseCursor: span.mouseCursor,
      onEnter: span.onEnter,
      onExit: span.onExit,
      semanticsLabel: span.semanticsLabel,
      locale: span.locale,
      spellOut: span.spellOut,
    );
  }
}

class _TwemojiWidgetSpan extends WidgetSpan {
  const _TwemojiWidgetSpan({required this.placeholder, required super.child})
    : super(alignment: PlaceholderAlignment.middle);

  final int placeholder;

  @override
  void computeToPlainText(
    StringBuffer buffer, {
    bool includeSemanticsLabels = true,
    bool includePlaceholders = true,
  }) {
    if (includePlaceholders) buffer.writeCharCode(placeholder);
  }
}

/// Returns selected source text for tests and selection adapters.
@visibleForTesting
String twemojiCopyTextForDisplayRange(
  InlineSpan sourceSpan,
  int displayStart,
  int displayEnd,
) {
  final builder = _TwemojiSpanBuilder(TextScaler.noScaling);
  builder.build(sourceSpan, const TextStyle());
  final range = builder.sourceRangeForDisplayRange(displayStart, displayEnd);
  final source = sourceSpan.toPlainText(includePlaceholders: true);
  return source.substring(
    range.start.clamp(0, source.length).toInt(),
    range.end.clamp(0, source.length).toInt(),
  );
}
