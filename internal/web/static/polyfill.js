if (!Array.prototype.at) {
  Array.prototype.at = function (i) { return i < 0 ? this[this.length + i] : this[i]; };
  String.prototype.at = function (i) { return i < 0 ? this[this.length + i] : this[i]; };
}
